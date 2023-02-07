package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	credentialspb "cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	dnsv2 "google.golang.org/api/dns/v2"
	oauthv2 "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
	sitev1 "google.golang.org/api/siteverification/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Ensure GoogleSiteVerificationProvider satisfies various provider interfaces.
var _ provider.Provider = &GoogleSiteVerificationProvider{}

// GoogleSiteVerificationProvider defines the provider implementation.
type GoogleSiteVerificationProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// GoogleSiteVerificationProviderModel describes the provider data model.
type GoogleSiteVerificationProviderModel struct {
	Project                   types.String `tfsdk:"project"`
	ImpersonateServiceAccount types.String `tfsdk:"impersonate_service_account"`
	TokenDuration             types.Int64  `tfsdk:"token_duration"`
}

type SiteVerificationClients struct {
	ProjectID        string
	DefaultOwner     string
	SiteVerification *sitev1.Service
	DNS              *dnsv2.Service
}

func (p *GoogleSiteVerificationProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "googlesiteverification"
	resp.Version = p.version
}

func (p *GoogleSiteVerificationProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project": schema.StringAttribute{
				MarkdownDescription: "The project ID to manage resources in. If it is not provided, the default project is used.",
				Optional:            true,
			},
			"impersonate_service_account": schema.StringAttribute{
				MarkdownDescription: "The service account ID to impersonate, if any. For more information on service account impersonation, see [the official documentation](https://cloud.google.com/iam/docs/impersonating-service-accounts).",
				Optional:            true,
				Required:            false,
			},
			"token_duration": schema.Int64Attribute{
				MarkdownDescription: "The duration of the token to impersonate the service account. If not set, the default duration of 1 hour will be used.",
				Optional:            true,
				Required:            false,
			},
		},
	}
}

func (p *GoogleSiteVerificationProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data GoogleSiteVerificationProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var defaultOwner string

	tflog.Trace(ctx, "Attempting to load default credentials")
	defaultCreds, err := google.FindDefaultCredentials(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to load default credentials", err.Error())
		return
	}
	creds := defaultCreds
	if !data.ImpersonateServiceAccount.IsNull() {
		defaultOwner = data.ImpersonateServiceAccount.ValueString()
		duration := int64(3600)
		if !data.TokenDuration.IsNull() {
			duration = data.TokenDuration.ValueInt64()
		}
		creds, err = impersonateServiceAccount(ctx, creds, data.ImpersonateServiceAccount.ValueString(), duration)
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to build credentials", err.Error())
		return
	}
	if !data.Project.IsNull() {
		defaultCreds.ProjectID = data.Project.String()
		creds.ProjectID = data.Project.String()
	}

	// Get site verification default identity
	if defaultOwner == "" {
		oauth2Service, err := oauthv2.NewService(ctx, option.WithCredentials(creds), option.WithScopes(oauthv2.OpenIDScope))
		if err != nil {
			resp.Diagnostics.AddError("Failed to create oauth2 client", err.Error())
			return
		}
		tokenInfo, err := oauth2Service.Tokeninfo().Context(ctx).Do()
		if err != nil {
			resp.Diagnostics.AddError("Failed to get token info", err.Error())
			return
		}
		defaultOwner = tokenInfo.Email
	}

	siteverificationService, err := sitev1.NewService(ctx,
		option.WithCredentials(creds),
		option.WithScopes(sitev1.SiteverificationScope, sitev1.SiteverificationVerifyOnlyScope))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create siteverification client", err.Error())
		return
	}
	dnsservice, err := dnsv2.NewService(ctx,
		option.WithCredentials(defaultCreds),
		option.WithScopes(dnsv2.NdevClouddnsReadwriteScope))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create dns client", err.Error())
		return
	}

	clients := &SiteVerificationClients{
		ProjectID:        creds.ProjectID,
		DefaultOwner:     defaultOwner,
		SiteVerification: siteverificationService,
		DNS:              dnsservice,
	}

	resp.DataSourceData = clients
	resp.ResourceData = clients
}

func (p *GoogleSiteVerificationProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSiteVerificationResource,
	}
}

func (p *GoogleSiteVerificationProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainKeyDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GoogleSiteVerificationProvider{
			version: version,
		}
	}
}

func impersonateServiceAccount(ctx context.Context, srcCreds *google.Credentials, serviceAccount string, durationSeconds int64) (*google.Credentials, error) {
	tflog.Trace(ctx, "Attempting to impersonate service account", map[string]any{
		"impersonate_service_account": serviceAccount,
	})
	c, err := credentials.NewIamCredentialsClient(ctx, option.WithCredentials(srcCreds))
	if err != nil {
		return nil, fmt.Errorf("failed to create credentials client: %w", err)
	}
	defer c.Close()
	req := &credentialspb.GenerateAccessTokenRequest{
		Name: fmt.Sprintf("projects/-/serviceAccounts/%s", serviceAccount),
		Scope: []string{
			sitev1.SiteverificationScope,
			sitev1.SiteverificationVerifyOnlyScope,
			dnsv2.NdevClouddnsReadwriteScope,
		},
		Lifetime: &durationpb.Duration{
			Seconds: durationSeconds,
		},
	}
	tflog.Trace(ctx, "Request", map[string]any{
		"request": req,
	})
	tokenresp, err := c.GenerateAccessToken(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token for service account: %w", err)
	}
	tflog.Trace(ctx, "Response", map[string]any{
		"expire_time": tokenresp.ExpireTime,
		"token":       tokenresp.AccessToken,
	})
	return &google.Credentials{
		TokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: tokenresp.AccessToken,
		}),
	}, nil
}
