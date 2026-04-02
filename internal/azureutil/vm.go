package azureutil

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
)

// ExtractPowerState finds the PowerState from a list of instance view statuses.
// Azure power state codes look like "PowerState/running", "PowerState/deallocated", etc.
func ExtractPowerState(statuses []*armcompute.InstanceViewStatus) string {
	for _, s := range statuses {
		if s.Code == nil {
			continue
		}

		if strings.HasPrefix(*s.Code, "PowerState/") {
			return strings.TrimPrefix(*s.Code, "PowerState/")
		}
	}

	return "unknown"
}

// TagValue returns the string value of an Azure resource tag, or "" if the tag
// is missing or nil.
func TagValue(tags map[string]*string, key string) string {
	if v, ok := tags[key]; ok && v != nil {
		return *v
	}

	return ""
}

// ResourceGroupID returns the full ARM resource ID for a resource group.
func ResourceGroupID(subscriptionID, rgName string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, rgName)
}
