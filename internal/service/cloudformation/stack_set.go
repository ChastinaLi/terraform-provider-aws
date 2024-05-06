// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloudformation

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/YakDriver/regexache"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	awstypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/hashicorp/aws-sdk-go-base/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/id"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/enum"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/flex"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @SDKResource("aws_cloudformation_stack_set", name="Stack Set")
// @Tags
func ResourceStackSet() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceStackSetCreate,
		ReadWithoutTimeout:   resourceStackSetRead,
		UpdateWithoutTimeout: resourceStackSetUpdate,
		DeleteWithoutTimeout: resourceStackSetDelete,

		Importer: &schema.ResourceImporter{
			StateContext: resourceStackSetImport,
		},

		Timeouts: &schema.ResourceTimeout{
			Update: schema.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"administration_role_arn": {
				Type:          schema.TypeString,
				Optional:      true,
				ConflictsWith: []string{"auto_deployment"},
				ValidateFunc:  verify.ValidARN,
			},
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"auto_deployment": {
				Type:     schema.TypeList,
				MinItems: 1,
				MaxItems: 1,
				Optional: true,
				ForceNew: true,
				ConflictsWith: []string{
					"administration_role_arn",
					"execution_role_name",
				},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"enabled": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"retain_stacks_on_account_removal": {
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
			"call_as": {
				Type:             schema.TypeString,
				Optional:         true,
				ValidateDiagFunc: enum.Validate[awstypes.CallAs](),
				Default:          awstypes.CallAsSelf,
			},
			"capabilities": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Schema{
					Type:             schema.TypeString,
					ValidateDiagFunc: enum.Validate[awstypes.Capability](),
				},
			},
			"description": {
				Type:         schema.TypeString,
				Optional:     true,
				ValidateFunc: validation.StringLenBetween(0, 1024),
			},
			"execution_role_name": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				ConflictsWith: []string{"auto_deployment"},
			},
			"managed_execution": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"active": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
					},
				},
				DiffSuppressFunc: verify.SuppressMissingOptionalConfigurationBlock,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.All(
					validation.StringLenBetween(1, 128),
					validation.StringMatch(regexache.MustCompile(`^[A-Za-z]`), "must begin with alphabetic character"),
					validation.StringMatch(regexache.MustCompile(`^[0-9A-Za-z-]+$`), "must contain only alphanumeric and hyphen characters"),
				),
			},
			"operation_preferences": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"failure_tolerance_count": {
							Type:          schema.TypeInt,
							Optional:      true,
							ValidateFunc:  validation.IntAtLeast(0),
							ConflictsWith: []string{"operation_preferences.0.failure_tolerance_percentage"},
						},
						"failure_tolerance_percentage": {
							Type:          schema.TypeInt,
							Optional:      true,
							ValidateFunc:  validation.IntBetween(0, 100),
							ConflictsWith: []string{"operation_preferences.0.failure_tolerance_count"},
						},
						"max_concurrent_count": {
							Type:          schema.TypeInt,
							Optional:      true,
							ValidateFunc:  validation.IntAtLeast(1),
							ConflictsWith: []string{"operation_preferences.0.max_concurrent_percentage"},
						},
						"max_concurrent_percentage": {
							Type:          schema.TypeInt,
							Optional:      true,
							ValidateFunc:  validation.IntBetween(1, 100),
							ConflictsWith: []string{"operation_preferences.0.max_concurrent_count"},
						},
						"region_concurrency_type": {
							Type:             schema.TypeString,
							Optional:         true,
							ValidateDiagFunc: enum.Validate[awstypes.RegionConcurrencyType](),
						},
						"region_order": {
							Type:     schema.TypeList,
							Optional: true,
							MinItems: 1,
							Elem: &schema.Schema{
								Type:         schema.TypeString,
								ValidateFunc: validation.StringMatch(regexache.MustCompile(`^[0-9A-Za-z-]{1,128}$`), ""),
							},
						},
					},
				},
			},
			"parameters": {
				Type:     schema.TypeMap,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"permission_model": {
				Type:             schema.TypeString,
				Optional:         true,
				ValidateDiagFunc: enum.Validate[awstypes.PermissionModels](),
				Default:          awstypes.PermissionModelsSelfManaged,
			},
			"stack_set_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			names.AttrTags:    tftags.TagsSchema(),
			names.AttrTagsAll: tftags.TagsSchemaComputed(),
			"template_body": {
				Type:             schema.TypeString,
				Optional:         true,
				Computed:         true,
				ConflictsWith:    []string{"template_url"},
				DiffSuppressFunc: verify.SuppressEquivalentJSONOrYAMLDiffs,
				ValidateFunc:     verify.ValidStringIsJSONOrYAML,
			},
			"template_url": {
				Type:          schema.TypeString,
				Optional:      true,
				ConflictsWith: []string{"template_body"},
			},
		},

		CustomizeDiff: verify.SetTagsDiff,
	}
}

func resourceStackSetCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).CloudFormationClient(ctx)

	name := d.Get("name").(string)
	input := &cloudformation.CreateStackSetInput{
		ClientRequestToken: aws.String(id.UniqueId()),
		StackSetName:       aws.String(name),
		Tags:               getTagsIn(ctx),
	}

	if v, ok := d.GetOk("administration_role_arn"); ok {
		input.AdministrationRoleARN = aws.String(v.(string))
	}

	if v, ok := d.GetOk("auto_deployment"); ok {
		input.AutoDeployment = expandAutoDeployment(v.([]interface{}))
	}

	if v, ok := d.GetOk("call_as"); ok {
		input.CallAs = awstypes.CallAs(v.(string))
	}

	if v, ok := d.GetOk("capabilities"); ok {
		input.Capabilities = flex.ExpandStringyValueSet[awstypes.Capability](v.(*schema.Set))
	}

	if v, ok := d.GetOk("description"); ok {
		input.Description = aws.String(v.(string))
	}

	if v, ok := d.GetOk("execution_role_name"); ok {
		input.ExecutionRoleName = aws.String(v.(string))
	}

	if v, ok := d.GetOk("managed_execution"); ok {
		input.ManagedExecution = expandManagedExecution(v.([]interface{}))
	}

	if v, ok := d.GetOk("parameters"); ok {
		input.Parameters = expandParameters(v.(map[string]interface{}))
	}

	if v, ok := d.GetOk("permission_model"); ok {
		input.PermissionModel = awstypes.PermissionModels(v.(string))
	}

	if v, ok := d.GetOk("template_body"); ok {
		input.TemplateBody = aws.String(v.(string))
	}

	if v, ok := d.GetOk("template_url"); ok {
		input.TemplateURL = aws.String(v.(string))
	}

	_, err := tfresource.RetryWhen(ctx, propagationTimeout,
		func() (interface{}, error) {
			output, err := conn.CreateStackSet(ctx, input)
			if err != nil {
				return nil, err
			}

			operation, err := WaitStackSetCreated(ctx, conn, name, d.Get("call_as").(string), d.Timeout(schema.TimeoutCreate))
			if err != nil {
				return nil, fmt.Errorf("waiting for completion (%s): %w", aws.ToString(output.StackSetId), err)
			}
			return operation, nil
		},
		func(err error) (bool, error) {
			if err == nil {
				return false, nil
			}

			message := err.Error()

			// IAM eventual consistency
			if strings.Contains(message, "AccountGate check failed") {
				return true, err
			}

			// IAM eventual consistency
			// User: XXX is not authorized to perform: cloudformation:CreateStack on resource: YYY
			if strings.Contains(message, "is not authorized") {
				return true, err
			}

			// IAM eventual consistency
			// XXX role has insufficient YYY permissions
			if strings.Contains(message, "role has insufficient") {
				return true, err
			}

			// IAM eventual consistency
			// Account XXX should have YYY role with trust relationship to Role ZZZ
			if strings.Contains(message, "role with trust relationship") {
				return true, err
			}

			// IAM eventual consistency
			if strings.Contains(message, "The security token included in the request is invalid") {
				return true, err
			}

			return false, err
		},
	)

	if err != nil {
		var detail string
		if tfawserr.ErrMessageContains(err, errCodeValidationError, "Account used is not a delegated administrator") {
			detail = "If you confirm that you are using a delegated administrator account, verify that the IAM User or Role has the permission \"organizations:ListDelegatedAdministrators\"."
		}

		d := errs.NewErrorDiagnostic(fmt.Sprintf("creating CloudFormation StackSet (%s): %s", name, err), detail)
		return append(diags, d)
	}

	d.SetId(name)

	return append(diags, resourceStackSetRead(ctx, d, meta)...)
}

func resourceStackSetRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).CloudFormationClient(ctx)

	callAs := d.Get("call_as").(string)
	stackSet, err := FindStackSetByName(ctx, conn, d.Id(), callAs)

	if !d.IsNewResource() && tfresource.NotFound(err) {
		log.Printf("[WARN] CloudFormation StackSet (%s) not found, removing from state", d.Id())
		d.SetId("")
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading CloudFormation StackSet (%s): %s", d.Id(), err)
	}

	d.Set("administration_role_arn", stackSet.AdministrationRoleARN)
	d.Set("arn", stackSet.StackSetARN)
	if err := d.Set("auto_deployment", flattenStackSetAutoDeploymentResponse(stackSet.AutoDeployment)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting auto_deployment: %s", err)
	}
	d.Set("capabilities", stackSet.Capabilities)
	d.Set("description", stackSet.Description)
	d.Set("execution_role_name", stackSet.ExecutionRoleName)
	if err := d.Set("managed_execution", flattenStackSetManagedExecution(stackSet.ManagedExecution)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting managed_execution: %s", err)
	}
	d.Set("name", stackSet.StackSetName)
	d.Set("permission_model", stackSet.PermissionModel)
	if err := d.Set("parameters", flattenAllParameters(stackSet.Parameters)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting parameters: %s", err)
	}
	d.Set("stack_set_id", stackSet.StackSetId)
	d.Set("template_body", stackSet.TemplateBody)

	setTagsOut(ctx, stackSet.Tags)

	return diags
}

func resourceStackSetUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).CloudFormationClient(ctx)

	input := &cloudformation.UpdateStackSetInput{
		OperationId:  aws.String(id.UniqueId()),
		StackSetName: aws.String(d.Id()),
		Tags:         []awstypes.Tag{},
		TemplateBody: aws.String(d.Get("template_body").(string)),
	}

	if v, ok := d.GetOk("administration_role_arn"); ok {
		input.AdministrationRoleARN = aws.String(v.(string))
	}

	callAs := d.Get("call_as").(string)
	if v, ok := d.GetOk("call_as"); ok {
		input.CallAs = awstypes.CallAs(v.(string))
	}

	if v, ok := d.GetOk("capabilities"); ok {
		input.Capabilities = flex.ExpandStringyValueSet[awstypes.Capability](v.(*schema.Set))
	}

	if v, ok := d.GetOk("description"); ok {
		input.Description = aws.String(v.(string))
	}

	if v, ok := d.GetOk("execution_role_name"); ok {
		input.ExecutionRoleName = aws.String(v.(string))
	}

	if v, ok := d.GetOk("managed_execution"); ok {
		input.ManagedExecution = expandManagedExecution(v.([]interface{}))
	}

	if v, ok := d.GetOk("operation_preferences"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		input.OperationPreferences = expandOperationPreferences(v.([]interface{})[0].(map[string]interface{}))
	}

	if v, ok := d.GetOk("parameters"); ok {
		input.Parameters = expandParameters(v.(map[string]interface{}))
	}

	if v, ok := d.GetOk("permission_model"); ok {
		input.PermissionModel = awstypes.PermissionModels(v.(string))
	}

	if tags := getTagsIn(ctx); len(tags) > 0 {
		input.Tags = tags
	}

	if v, ok := d.GetOk("template_url"); ok {
		// ValidationError: Exactly one of TemplateBody or TemplateUrl must be specified
		// TemplateBody is always present when TemplateUrl is used so remove TemplateBody if TemplateUrl is set
		input.TemplateBody = nil
		input.TemplateURL = aws.String(v.(string))
	}

	// When `auto_deployment` is set, ignore `administration_role_arn` and
	// `execution_role_name` fields since it's using the SERVICE_MANAGED
	// permission model
	if v, ok := d.GetOk("auto_deployment"); ok {
		input.AdministrationRoleARN = nil
		input.ExecutionRoleName = nil
		input.AutoDeployment = expandAutoDeployment(v.([]interface{}))
	}

	output, err := conn.UpdateStackSet(ctx, input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "updating CloudFormation StackSet (%s): %s", d.Id(), err)
	}

	if _, err := WaitStackSetOperationSucceeded(ctx, conn, d.Id(), aws.ToString(output.OperationId), callAs, d.Timeout(schema.TimeoutUpdate)); err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for CloudFormation StackSet (%s) update: %s", d.Id(), err)
	}

	return append(diags, resourceStackSetRead(ctx, d, meta)...)
}

func resourceStackSetDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).CloudFormationClient(ctx)

	input := &cloudformation.DeleteStackSetInput{
		StackSetName: aws.String(d.Id()),
	}

	if v, ok := d.GetOk("call_as"); ok {
		input.CallAs = awstypes.CallAs(v.(string))
	}

	log.Printf("[DEBUG] Deleting CloudFormation StackSet: %s", d.Id())
	_, err := conn.DeleteStackSet(ctx, input)

	if errs.IsA[*awstypes.StackSetNotFoundException](err) {
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting CloudFormation StackSet (%s): %s", d.Id(), err)
	}

	return diags
}

func resourceStackSetImport(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	const stackSetImportIDSeparator = ","

	switch parts := strings.Split(d.Id(), stackSetImportIDSeparator); len(parts) {
	case 1:
	case 2:
		d.SetId(parts[0])
		d.Set("call_as", parts[1])
	default:
		return []*schema.ResourceData{}, fmt.Errorf("unexpected format for import ID (%[1]s), use: STACKSETNAME or STACKSETNAME%[2]sCALLAS", d.Id(), stackSetImportIDSeparator)
	}

	return []*schema.ResourceData{d}, nil
}

func expandAutoDeployment(l []interface{}) *awstypes.AutoDeployment {
	if len(l) == 0 {
		return nil
	}

	m := l[0].(map[string]interface{})

	enabled := m["enabled"].(bool)
	autoDeployment := &awstypes.AutoDeployment{
		Enabled: aws.Bool(enabled),
	}

	if enabled {
		autoDeployment.RetainStacksOnAccountRemoval = aws.Bool(m["retain_stacks_on_account_removal"].(bool))
	}

	return autoDeployment
}

func expandManagedExecution(l []interface{}) *awstypes.ManagedExecution {
	if len(l) == 0 {
		return nil
	}

	m := l[0].(map[string]interface{})

	managedExecution := &awstypes.ManagedExecution{
		Active: aws.Bool(m["active"].(bool)),
	}

	return managedExecution
}

func flattenStackSetAutoDeploymentResponse(autoDeployment *awstypes.AutoDeployment) []map[string]interface{} {
	if autoDeployment == nil {
		return []map[string]interface{}{}
	}

	m := map[string]interface{}{
		"enabled":                          aws.ToBool(autoDeployment.Enabled),
		"retain_stacks_on_account_removal": aws.ToBool(autoDeployment.RetainStacksOnAccountRemoval),
	}

	return []map[string]interface{}{m}
}

func flattenStackSetManagedExecution(managedExecution *awstypes.ManagedExecution) []map[string]interface{} {
	if managedExecution == nil {
		return []map[string]interface{}{}
	}

	m := map[string]interface{}{
		"active": aws.ToBool(managedExecution.Active),
	}

	return []map[string]interface{}{m}
}
