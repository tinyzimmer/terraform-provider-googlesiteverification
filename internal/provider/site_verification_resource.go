package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	dnsv2 "google.golang.org/api/dns/v2"
	sitev1 "google.golang.org/api/siteverification/v1"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &SiteVerificationResource{}
var _ resource.ResourceWithImportState = &SiteVerificationResource{}

func NewSiteVerificationResource() resource.Resource {
	return &SiteVerificationResource{}
}

// SiteVerificationResource defines the resource implementation.
type SiteVerificationResource struct {
	Clients *SiteVerificationClients
}

// SiteVerificationResourceModel describes the resource data model.
type SiteVerificationResourceModel struct {
	Project            types.String `tfsdk:"project"`
	VerificationMethod types.String `tfsdk:"verification_method"`
	SiteIdentifier     types.String `tfsdk:"site_identifier"`
	SiteType           types.String `tfsdk:"site_type"`
	Token              types.String `tfsdk:"token"`
	ManagedZone        types.String `tfsdk:"managed_zone"`
	Owners             types.List   `tfsdk:"owners"`
	ID                 types.String `tfsdk:"id"`
}

func (s *SiteVerificationResourceModel) EncodedID() string {
	return url.PathEscape(s.ID.ValueString())
}

func (s *SiteVerificationResourceModel) SiteID() string {
	return strings.TrimSuffix(s.SiteIdentifier.ValueString(), ".")
}

func (r *SiteVerificationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_site_verification"
}

func (r *SiteVerificationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Attempts to verify a domain.",

		Attributes: map[string]schema.Attribute{
			"project": schema.StringAttribute{
				MarkdownDescription: "The project to use for verification. Defaults to the provider project.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"verification_method": schema.StringAttribute{
				MarkdownDescription: "The verification method to use. Defaults to DNS_TXT.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site_identifier": schema.StringAttribute{
				MarkdownDescription: "The DNS name or URL to retrieve a verification token for.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site_type": schema.StringAttribute{
				MarkdownDescription: "The type of site verification to attempt. Defaults to INET_DOMAIN.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "The verification token.",
				Required:            true,
			},
			"managed_zone": schema.StringAttribute{
				MarkdownDescription: "The managed zone to use for DNS verification. Defaults to the first managed zone found.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"owners": schema.ListAttribute{
				MarkdownDescription: "The owners of the site. Defaults to the current user.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
					// listplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the site.",
				Computed:            true,
			},
		},
	}
}

func (r *SiteVerificationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	r.Clients = data
}

func (r *SiteVerificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *SiteVerificationResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.SiteType.IsNull() {
		data.SiteType = types.StringValue("INET_DOMAIN")
	}

	if data.VerificationMethod.IsNull() {
		data.VerificationMethod = types.StringValue("DNS_TXT")
	}

	if data.SiteType.ValueString() == "INET_DOMAIN" {
		if data.VerificationMethod.ValueString() == "DNS_TXT" {
			if data.Project.IsNull() {
				data.Project = types.StringValue(r.Clients.ProjectID)
			}
			err := r.createDNSRecord(ctx, data)
			if err != nil {
				resp.Diagnostics.AddError("Error creating DNS record", err.Error())
				return
			}
		}
	}

	err := r.insertSiteVerification(ctx, resp.Diagnostics, data)
	if err != nil {
		resp.Diagnostics.AddError("Error inserting site verification", err.Error())
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SiteVerificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *SiteVerificationResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.SiteType.ValueString() == "INET_DOMAIN" {
		if data.VerificationMethod.ValueString() == "DNS_TXT" {
			tflog.Trace(ctx, "Looking up TXT verification record for name", map[string]any{"name": data.SiteIdentifier.ValueString(), "zone": data.ManagedZone.ValueString()})
			err := r.readDNSRecord(ctx, data)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					tflog.Trace(ctx, "DNS TXT Record not found", map[string]any{"id": data.ID.String()})
					resp.State.RemoveResource(ctx)
					return
				}
				resp.Diagnostics.AddError("Error reading DNS record", err.Error())
				return
			}
		}
	}

	tflog.Trace(ctx, "Looking up site verification", map[string]any{
		"id":   data.ID.String(),
		"site": data.SiteIdentifier.ValueString(),
	})
	err := r.readSiteVerification(ctx, resp.Diagnostics, data)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			tflog.Trace(ctx, "Site verification not found", map[string]any{"id": data.ID.String()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading site verification", err.Error())
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SiteVerificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *SiteVerificationResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	tflog.Trace(ctx, "Site Verification Upate Plan", map[string]any{
		"token":  data.Token.ValueString(),
		"owners": data.Owners.String(),
	})

	if resp.Diagnostics.HasError() {
		return
	}

	if data.SiteType.IsNull() {
		data.SiteType = types.StringValue("INET_DOMAIN")
	}

	if data.VerificationMethod.IsNull() {
		data.VerificationMethod = types.StringValue("DNS_TXT")
	}

	if data.SiteType.ValueString() == "INET_DOMAIN" {
		if data.VerificationMethod.ValueString() == "DNS_TXT" {
			err := r.deleteDNSRecord(ctx, data)
			if err != nil {
				resp.Diagnostics.AddError("Error deleting DNS record", err.Error())
				return
			}
			err = r.createDNSRecord(ctx, data)
			if err != nil {
				resp.Diagnostics.AddError("Error updating DNS TXT record", err.Error())
				return
			}
		}
	}

	err := r.patchSiteVerification(ctx, resp.Diagnostics, data)
	if err != nil {
		resp.Diagnostics.AddError("Error updating site verification", err.Error())
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SiteVerificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *SiteVerificationResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.SiteType.ValueString() == "INET_DOMAIN" {
		if data.VerificationMethod.ValueString() == "DNS_TXT" {
			err := r.deleteDNSRecord(ctx, data)
			if err != nil {
				resp.Diagnostics.AddError("Error deleting DNS record", err.Error())
				return
			}
			tflog.Trace(ctx, "DNS record deleted")
		}
	}

	err := r.deleteSiteVerification(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Error relinquishing site verification", err.Error())
	}
}

func (r *SiteVerificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *SiteVerificationResource) createDNSRecord(ctx context.Context, data *SiteVerificationResourceModel) error {
	record := &dnsv2.ResourceRecordSet{
		Name:    forceDot(data.SiteIdentifier.ValueString()),
		Rrdatas: []string{data.Token.ValueString()},
		Ttl:     60,
		Type:    "TXT",
	}
	tflog.Trace(ctx, "Creating DNS record", map[string]any{
		"id":      data.ID.String(),
		"site":    data.SiteIdentifier.ValueString(),
		"zone":    data.ManagedZone.ValueString(),
		"project": data.Project.ValueString(),
		"token":   data.Token.ValueString(),
		"record":  record,
	})
	gresp, err := r.Clients.DNS.ResourceRecordSets.Create(
		data.Project.ValueString(),
		"global",
		data.ManagedZone.ValueString(),
		record,
	).Context(ctx).Do()
	if err != nil {
		return err
	}
	tflog.Trace(ctx, "DNS record created", map[string]any{
		"response": gresp,
	})
	return nil
}

func (r *SiteVerificationResource) readDNSRecord(ctx context.Context, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Looking up DNS record", map[string]any{
		"id":      data.ID.String(),
		"site":    data.SiteIdentifier.ValueString(),
		"zone":    data.ManagedZone.ValueString(),
		"project": data.Project.ValueString(),
	})
	gresp, err := r.Clients.DNS.ResourceRecordSets.Get(
		data.Project.ValueString(),
		"global",
		data.ManagedZone.ValueString(),
		data.SiteIdentifier.ValueString(),
		"TXT",
	).Context(ctx).Do()
	if err != nil {
		return err
	}
	if len(gresp.Rrdatas) != 1 {
		return fmt.Errorf("Expected 1 TXT record, got %d", len(gresp.Rrdatas))
	}
	data.Token = types.StringValue(strings.Trim(gresp.Rrdatas[0], `"`))
	return nil
}

func (r *SiteVerificationResource) deleteDNSRecord(ctx context.Context, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Deleting DNS record", map[string]any{
		"id":      data.ID.String(),
		"site":    data.SiteIdentifier.ValueString(),
		"zone":    data.ManagedZone.ValueString(),
		"project": data.Project.ValueString(),
	})
	err := r.Clients.DNS.ResourceRecordSets.Delete(
		data.Project.ValueString(),
		"global",
		data.ManagedZone.ValueString(),
		data.SiteIdentifier.ValueString(),
		"TXT",
	).Context(ctx).Do()
	if err != nil {
		return err
	}
	return nil
}

func (r *SiteVerificationResource) insertSiteVerification(ctx context.Context, diag diag.Diagnostics, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Inserting site verification", map[string]any{
		"id":   data.ID.String(),
		"site": data.SiteIdentifier.ValueString(),
	})
	greq, err := r.buildSiteVerification(ctx, data, true)
	if err != nil {
		return err
	}
	tflog.Trace(ctx, "Request", map[string]any{
		"request": greq,
	})
	callResp, err := r.Clients.SiteVerification.WebResource.Insert(data.VerificationMethod.ValueString(), greq).Context(ctx).Do()
	if err != nil {
		return err
	}

	tflog.Trace(ctx, "Response", map[string]any{
		"status": callResp.ServerResponse.HTTPStatusCode,
		"id":     callResp.Id,
	})
	// Extract owners from response
	owners, diags := parseOwnersFromResponse(callResp)
	diag.Append(diags...)
	data.Owners = owners
	// Extract ID from response
	id, err := decodeID(callResp.Id)
	if err != nil {
		return err
	}
	data.ID = types.StringValue(id)
	tflog.Trace(ctx, "Created site verification")
	return nil
}

func (r *SiteVerificationResource) readSiteVerification(ctx context.Context, diag diag.Diagnostics, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Looking up site verification", map[string]any{
		"id":   data.ID.String(),
		"site": data.SiteIdentifier.ValueString(),
	})
	resp, err := r.Clients.SiteVerification.WebResource.Get(data.SiteID()).Context(ctx).Do()
	if err != nil {
		return err
	}
	tflog.Trace(ctx, "Read Site Verification", map[string]any{
		"status": resp.ServerResponse.HTTPStatusCode,
		"owners": resp.Owners,
	})
	owners, diags := parseOwnersFromResponse(resp)
	diag.Append(diags...)
	data.Owners = owners
	return nil
}

func (r *SiteVerificationResource) patchSiteVerification(ctx context.Context, diag diag.Diagnostics, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Patching site verification", map[string]any{
		"id":   data.ID.String(),
		"site": data.SiteIdentifier.ValueString(),
	})
	greq, err := r.buildSiteVerification(ctx, data, false)
	if err != nil {
		return err
	}
	tflog.Trace(ctx, "Request", map[string]any{
		"request": greq,
	})
	callResp, err := r.Clients.SiteVerification.WebResource.Patch(data.SiteID(), greq).Context(ctx).Do()
	if err != nil {
		return err
	}
	tflog.Trace(ctx, "Response", map[string]any{
		"status": callResp.ServerResponse.HTTPStatusCode,
		"id":     callResp.Id,
	})
	// Extract owners from response
	owns, diags := stringSliceToListValue(greq.Owners)
	diag.Append(diags...)
	data.Owners = owns
	return nil
}

func (r *SiteVerificationResource) buildSiteVerification(ctx context.Context, data *SiteVerificationResourceModel, includeSite bool) (*sitev1.SiteVerificationWebResourceResource, error) {
	greq := &sitev1.SiteVerificationWebResourceResource{}
	if includeSite {
		greq.Site = &sitev1.SiteVerificationWebResourceResourceSite{
			Identifier: data.SiteIdentifier.ValueString(),
			Type:       data.SiteType.ValueString(),
		}
	}
	if !data.Owners.IsNull() {
		owners, err := parseOwnersFromData(ctx, data)
		if err != nil {
			return nil, err
		}
		greq.Owners = append(greq.Owners, owners...)
	}
	return greq, nil
}

func (r *SiteVerificationResource) deleteSiteVerification(ctx context.Context, data *SiteVerificationResourceModel) error {
	tflog.Trace(ctx, "Deleting site verification for id", map[string]any{
		"id":   data.ID.ValueString(),
		"site": data.SiteIdentifier.ValueString(),
	})
	return r.Clients.SiteVerification.WebResource.Delete(data.SiteID()).Context(ctx).Do()
}
