package sumologic

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
)

func resourceSumologicGenericPollingSource() *schema.Resource {
	pollingSource := resourceSumologicSource()
	pollingSource.Create = resourceSumologicGenericPollingSourceCreate
	pollingSource.Read = resourceSumologicGenericPollingSourceRead
	pollingSource.Update = resourceSumologicGenericPollingSourceUpdate
	pollingSource.Importer = &schema.ResourceImporter{
		State: resourceSumologicSourceImport,
	}

	pollingSource.Schema["content_type"] = &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
		ForceNew: true,
		ValidateFunc: validation.StringInSlice([]string{"AwsS3Bucket", "AwsElbBucket", "AwsCloudFrontBucket",
			"AwsCloudTrailBucket", "AwsS3AuditBucket", "AwsCloudWatch", "AwsInventory", "AwsXRay"}, false),
	}
	pollingSource.Schema["scan_interval"] = &schema.Schema{
		Type:     schema.TypeInt,
		Required: true,
	}
	pollingSource.Schema["paused"] = &schema.Schema{
		Type:     schema.TypeBool,
		Required: true,
	}
	pollingSource.Schema["url"] = &schema.Schema{
		Type:     schema.TypeString,
		Computed: true,
	}
	pollingSource.Schema["authentication"] = &schema.Schema{
		Type:     schema.TypeList,
		Required: true,
		ForceNew: true,
		MinItems: 1,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"type": {
					Type:         schema.TypeString,
					Required:     true,
					ValidateFunc: validation.StringInSlice([]string{"S3BucketAuthentication", "AWSRoleBasedAuthentication"}, false),
				},
				"access_key": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"secret_key": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"role_arn": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"region": {
					Type:     schema.TypeString,
					Optional: true,
				},
			},
		},
	}
	pollingSource.Schema["path"] = &schema.Schema{
		Type:     schema.TypeList,
		Required: true,
		ForceNew: true,
		MinItems: 1,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"type": {
					Type:     schema.TypeString,
					Required: true,
					ValidateFunc: validation.StringInSlice([]string{"S3BucketPathExpression", "CloudWatchPath",
						"AwsInventoryPath", "AwsXRayPath"}, false),
				},
				"bucket_name": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"path_expression": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"limit_to_regions": {
					Type:     schema.TypeList,
					Optional: true,
					Elem: &schema.Schema{
						Type: schema.TypeString,
					},
				},
				"limit_to_namespaces": {
					Type:     schema.TypeList,
					Optional: true,
					Elem: &schema.Schema{
						Type: schema.TypeString,
					},
				},

				"tag_filters": {
					Type:     schema.TypeList,
					Optional: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"type": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"namespace": {
								Type:     schema.TypeString,
								Optional: true,
							},
							"tags": {
								Type:     schema.TypeList,
								Optional: true,
								Elem: &schema.Schema{
									Type: schema.TypeString,
								},
							},
						},
					},
				},
			},
		},
	}

	return pollingSource
}

func resourceSumologicGenericPollingSourceCreate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	if d.Id() == "" {
		source, err := resourceToGenericPollingSource(d)
		if err != nil {
			return err
		}

		sourceID, err := c.CreatePollingSource(source, d.Get("collector_id").(int))
		if err != nil {
			return err
		}

		d.SetId(strconv.Itoa(sourceID))
	}

	return resourceSumologicGenericPollingSourceRead(d, meta)
}

func resourceSumologicGenericPollingSourceUpdate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	source, err := resourceToGenericPollingSource(d)
	if err != nil {
		return err
	}

	err = c.UpdatePollingSource(source, d.Get("collector_id").(int))
	if err != nil {
		return err
	}

	return resourceSumologicGenericPollingSourceRead(d, meta)
}

func resourceSumologicGenericPollingSourceRead(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	id, _ := strconv.Atoi(d.Id())
	source, err := c.GetPollingSource(d.Get("collector_id").(int), id)

	if err != nil {
		return err
	}

	if source == nil {
		log.Printf("[WARN] Polling source not found, removing from state: %v - %v", id, err)
		d.SetId("")

		return nil
	}

	pollingResources := source.ThirdPartyRef.Resources
	path := getPollingThirdPartyPathAttributes(pollingResources)

	if err := d.Set("path", path); err != nil {
		return err
	}

	authSettings := getPollingThirdPartyAuthenticationAttributes(pollingResources)
	if err := d.Set("authentication", authSettings); err != nil {
		return err
	}

	if err := resourceSumologicSourceRead(d, source.Source); err != nil {
		return fmt.Errorf("%s", err)
	}
	d.Set("content_type", source.ContentType)
	d.Set("scan_interval", source.ScanInterval)
	d.Set("paused", source.Paused)
	d.Set("url", source.URL)

	return nil
}

func resourceToGenericPollingSource(d *schema.ResourceData) (PollingSource, error) {
	source := resourceToSource(d)
	source.Type = "Polling"

	pollingSource := PollingSource{
		Source:       source,
		Paused:       d.Get("paused").(bool),
		ScanInterval: d.Get("scan_interval").(int),
		ContentType:  d.Get("content_type").(string),
		URL:          d.Get("url").(string),
	}

	authSettings, errAuthSettings := getPollingAuthentication(d)
	if errAuthSettings != nil {
		return pollingSource, errAuthSettings
	}

	pathSettings, errPathSettings := getPollingPathSettings(d)
	if errPathSettings != nil {
		return pollingSource, errPathSettings
	}

	pollingResource := PollingResource{
		ServiceType:    d.Get("content_type").(string),
		Authentication: authSettings,
		Path:           pathSettings,
	}

	pollingSource.ThirdPartyRef.Resources = append(pollingSource.ThirdPartyRef.Resources, pollingResource)

	return pollingSource, nil
}

func getPollingThirdPartyPathAttributes(pollingResource []PollingResource) []map[string]interface{} {

	var s []map[string]interface{}

	for _, t := range pollingResource {
		mapping := map[string]interface{}{
			"type":                t.Path.Type,
			"bucket_name":         t.Path.BucketName,
			"path_expression":     t.Path.PathExpression,
			"limit_to_regions":    t.Path.LimitToRegions,
			"limit_to_namespaces": t.Path.LimitToNamespaces,
			"tag_filters":         flattenPollingTagFilters(t.Path.TagFilters),
		}
		s = append(s, mapping)
	}
	return s
}

func getPollingThirdPartyAuthenticationAttributes(pollingResource []PollingResource) []map[string]interface{} {

	var s []map[string]interface{}

	for _, t := range pollingResource {
		mapping := map[string]interface{}{
			"type":       t.Authentication.Type,
			"access_key": t.Authentication.AwsID,
			"secret_key": t.Authentication.AwsKey,
			"role_arn":   t.Authentication.RoleARN,
			"region":     t.Authentication.Region,
		}
		s = append(s, mapping)
	}
	return s
}

func flattenPollingTagFilters(v []TagFilter) []map[string]interface{} {
	var filters []map[string]interface{}
	for _, d := range v {
		filter := map[string]interface{}{
			"type":      d.Type,
			"namespace": d.Namespace,
			"tags":      d.Tags,
		}
		filters = append(filters, filter)
	}

	return filters
}

func getPollingTagFilters(d *schema.ResourceData) []TagFilter {
	paths := d.Get("path").([]interface{})
	path := paths[0].(map[string]interface{})
	rawTagFilterConfig := path["tag_filters"].([]interface{})
	var filters []TagFilter

	for _, rawConfig := range rawTagFilterConfig {
		config := rawConfig.(map[string]interface{})
		filter := TagFilter{}
		filter.Type = config["type"].(string)
		filter.Namespace = config["namespace"].(string)

		rawTags := config["tags"].([]interface{})
		Tags := make([]string, len(rawTags))
		for i, v := range rawTags {
			Tags[i] = v.(string)
		}
		filter.Tags = Tags
		filters = append(filters, filter)
	}

	return filters
}

func getPollingAuthentication(d *schema.ResourceData) (PollingAuthentication, error) {
	auths := d.Get("authentication").([]interface{})
	authSettings := PollingAuthentication{}

	if len(auths) > 0 {
		auth := auths[0].(map[string]interface{})
		switch authType := auth["type"].(string); authType {
		case "S3BucketAuthentication":
			if d.Get("content_type").(string) == "AwsInventory" {
				return authSettings, errors.New(
					fmt.Sprintf("[ERROR] Unsupported authType: %v for AwsInventory source", authType))
			}
			authSettings.Type = "S3BucketAuthentication"
			authSettings.AwsID = auth["access_key"].(string)
			authSettings.AwsKey = auth["secret_key"].(string)
			if auth["region"] != nil {
				authSettings.Region = auth["region"].(string)
			}
		case "AWSRoleBasedAuthentication":
			authSettings.Type = "AWSRoleBasedAuthentication"
			authSettings.RoleARN = auth["role_arn"].(string)
			if auth["region"] != nil {
				authSettings.Region = auth["region"].(string)
			}
		default:
			errorMessage := fmt.Sprintf("[ERROR] Unknown authType: %v", authType)
			log.Print(errorMessage)
			return authSettings, errors.New(errorMessage)
		}
	}

	return authSettings, nil
}

func getPollingPathSettings(d *schema.ResourceData) (PollingPath, error) {
	pathSettings := PollingPath{}
	paths := d.Get("path").([]interface{})

	if len(paths) > 0 {
		path := paths[0].(map[string]interface{})
		switch pathType := path["type"].(string); pathType {
		case "S3BucketPathExpression":
			pathSettings.Type = "S3BucketPathExpression"
			pathSettings.BucketName = path["bucket_name"].(string)
			pathSettings.PathExpression = path["path_expression"].(string)
		case "CloudWatchPath", "AwsInventoryPath":
			pathSettings.Type = pathType
			rawLimitToRegions := path["limit_to_regions"].([]interface{})
			LimitToRegions := make([]string, 0, len(rawLimitToRegions))
			for _, v := range rawLimitToRegions {
				if v != nil {
					LimitToRegions = append(LimitToRegions, v.(string))
				}
			}

			rawLimitToNamespaces := path["limit_to_namespaces"].([]interface{})
			LimitToNamespaces := make([]string, 0, len(rawLimitToNamespaces))
			for _, v := range rawLimitToNamespaces {
				if v != nil {
					LimitToNamespaces = append(LimitToNamespaces, v.(string))
				}
			}
			pathSettings.LimitToRegions = LimitToRegions
			pathSettings.LimitToNamespaces = LimitToNamespaces
			if pathType == "CloudWatchPath" {
				pathSettings.TagFilters = getPollingTagFilters(d)
			}
		case "AwsXRayPath":
			pathSettings.Type = "AwsXRayPath"
			rawLimitToRegions := path["limit_to_regions"].([]interface{})
			LimitToRegions := make([]string, 0, len(rawLimitToRegions))
			for _, v := range rawLimitToRegions {
				if v != nil {
					LimitToRegions = append(LimitToRegions, v.(string))
				}
			}
			pathSettings.LimitToRegions = LimitToRegions
		default:
			errorMessage := fmt.Sprintf("[ERROR] Unknown resourceType in path: %v", pathType)
			log.Print(errorMessage)
			return pathSettings, errors.New(errorMessage)
		}
	}

	return pathSettings, nil
}
