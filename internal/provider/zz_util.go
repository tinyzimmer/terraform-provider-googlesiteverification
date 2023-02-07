package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	sitev1 "google.golang.org/api/siteverification/v1"
)

func stringSliceToListValue(vals []string) (basetypes.ListValue, diag.Diagnostics) {
	var listVals []attr.Value
	for _, val := range vals {
		listVals = append(listVals, types.StringValue(val))
	}
	return types.ListValue(types.StringType, listVals)
}

func stringSliceToQuotedListValue(vals []string) (basetypes.ListValue, diag.Diagnostics) {
	var listVals []attr.Value
	for _, val := range vals {
		listVals = append(listVals, types.StringValue(fmt.Sprintf("%q", val)))
	}
	return types.ListValue(types.StringType, listVals)
}

func listValueToStringSlice(ctx context.Context, list basetypes.ListValue) ([]string, error) {
	var vals []string
	for _, elem := range list.Elements() {
		val, err := elem.ToTerraformValue(ctx)
		if err != nil {
			return nil, err
		}
		var str string
		if err := val.As(&str); err != nil {
			return nil, err
		}
		vals = append(vals, str)
	}
	return vals, nil
}

func parseOwnersFromResponse(resp *sitev1.SiteVerificationWebResourceResource) (basetypes.ListValue, diag.Diagnostics) {
	var vals []attr.Value
	for _, owner := range resp.Owners {
		vals = append(vals, types.StringValue(owner))
	}
	return types.ListValue(types.StringType, vals)
}

func parseOwnersFromData(ctx context.Context, data *SiteVerificationResourceModel) ([]string, error) {
	if data.Owners.IsNull() {
		return nil, nil
	}
	return listValueToStringSlice(ctx, data.Owners)
}

func decodeID(id string) (string, error) {
	return url.PathUnescape(id)
}

func forceDot(str string) string {
	if strings.HasSuffix(str, ".") {
		return str
	}
	return fmt.Sprintf("%s.", str)
}
