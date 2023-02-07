package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	sitev1 "google.golang.org/api/siteverification/v1"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &DomainKeyDataSource{}

func NewDomainKeyDataSource() datasource.DataSource {
	return &DomainKeyDataSource{}
}

// DomainKeyDataSource defines the data source implementation.
type DomainKeyDataSource struct {
	client *sitev1.Service
}

// DomainKeyDataSourceModel describes the data source data model.
type DomainKeyDataSourceModel struct {
	VerificationMethod types.String `tfsdk:"verification_method"`
	SiteIdentifier     types.String `tfsdk:"site_identifier"`
	SiteType           types.String `tfsdk:"site_type"`
	Token              types.String `tfsdk:"token"`
}

func (d *DomainKeyDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_key"
}

func (d *DomainKeyDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Retrieves a verification token for a domain.",

		Attributes: map[string]schema.Attribute{
			"verification_method": schema.StringAttribute{
				MarkdownDescription: "The verification method to use. Defaults to DNS_TXT.",
				Optional:            true,
				Computed:            true,
			},
			"site_identifier": schema.StringAttribute{
				MarkdownDescription: "The DNS name or URL to retrieve a verification token for.",
				Required:            true,
			},
			"site_type": schema.StringAttribute{
				MarkdownDescription: "The type of site verification to attempt. Defaults to INET_DOMAIN.",
				Optional:            true,
				Computed:            true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The verification token to use for the site.",
				Computed:            true,
			},
		},
	}
}

func (d *DomainKeyDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*SiteVerificationClients)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *http.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = data.SiteVerification
}

func (d *DomainKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data DomainKeyDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.SiteType.IsNull() {
		data.SiteType = types.StringValue("INET_DOMAIN")
	}

	if data.VerificationMethod.IsNull() {
		data.VerificationMethod = types.StringValue("DNS_TXT")
	}

	greq := &sitev1.SiteVerificationWebResourceGettokenRequest{
		Site: &sitev1.SiteVerificationWebResourceGettokenRequestSite{
			Identifier: data.SiteIdentifier.ValueString(),
			Type:       data.SiteType.ValueString(),
		},
		VerificationMethod: data.VerificationMethod.ValueString(),
	}
	tflog.Trace(ctx, "Request", map[string]any{
		"request": greq,
	})

	callResp, err := d.client.WebResource.GetToken(greq).Context(ctx).Do()
	if err != nil {
		resp.Diagnostics.AddError("Error retrieving verification token", err.Error())
		return
	}

	tflog.Trace(ctx, "Response", map[string]any{
		"status": callResp.ServerResponse.HTTPStatusCode,
		"header": callResp.ServerResponse.Header,
		"token":  callResp.Token,
	})

	data.Token = types.StringValue(callResp.Token)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
