package app

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadConfig(path string) (*AppConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *AppConfig) Validate() error {
	if c.App.Name == "" {
		return fmt.Errorf("app.name is required")
	}
	if len(c.Nodes) == 0 {
		return fmt.Errorf("at least one node is required")
	}

	nodeIDs := map[string]bool{}
	for _, node := range c.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node id is required")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node id: %s", node.ID)
		}
		nodeIDs[node.ID] = true
	}
	if err := c.Diagnostics.ResourceThresholds.Validate(nodeIDs); err != nil {
		return err
	}

	projectIDs := map[string]bool{}
	for _, project := range c.Projects {
		if project.ID == "" {
			return fmt.Errorf("project id is required")
		}
		if projectIDs[project.ID] {
			return fmt.Errorf("duplicate project id: %s", project.ID)
		}
		projectIDs[project.ID] = true
	}

	serviceIDs := map[string]bool{}
	for _, service := range c.Services {
		if service.ID == "" {
			return fmt.Errorf("service id is required")
		}
		if serviceIDs[service.ID] {
			return fmt.Errorf("duplicate service id: %s", service.ID)
		}
		serviceIDs[service.ID] = true
		if !nodeIDs[service.NodeID] {
			return fmt.Errorf("service %s references missing node %s", service.ID, service.NodeID)
		}
		if service.ProjectID != "" && !projectIDs[service.ProjectID] {
			return fmt.Errorf("service %s references missing project %s", service.ID, service.ProjectID)
		}
		if service.ResourceBudget != nil {
			if len(service.ResourceBudget.ContainerNames) == 0 && service.ResourceBudget.ComposeProject == "" {
				return fmt.Errorf("service %s resource_budget requires container_names or compose_project", service.ID)
			}
			for _, name := range service.ResourceBudget.ContainerNames {
				if strings.TrimSpace(name) == "" {
					return fmt.Errorf("service %s resource_budget has an empty container name", service.ID)
				}
			}
			if service.ResourceBudget.MaxMemoryMiB < 0 {
				return fmt.Errorf("service %s resource_budget.max_memory_mib must be non-negative", service.ID)
			}
			if service.ResourceBudget.MaxCPUPercent < 0 {
				return fmt.Errorf("service %s resource_budget.max_cpu_percent must be non-negative", service.ID)
			}
			if service.ResourceBudget.MaxMemoryMiB == 0 && service.ResourceBudget.MaxCPUPercent == 0 {
				return fmt.Errorf("service %s resource_budget requires max_memory_mib or max_cpu_percent", service.ID)
			}
		}
	}

	for _, project := range c.Projects {
		for _, serviceID := range project.ServiceIDs {
			if !serviceIDs[serviceID] {
				return fmt.Errorf("project %s references missing service %s", project.ID, serviceID)
			}
		}
	}

	return nil
}

func (c ResourceThresholdConfig) Validate(nodeIDs map[string]bool) error {
	checks := []struct {
		name  string
		value *float64
	}{
		{name: "diagnostics.resource_thresholds.cpu_percent", value: c.CPUPercent},
		{name: "diagnostics.resource_thresholds.memory_percent", value: c.MemoryPercent},
		{name: "diagnostics.resource_thresholds.root_disk_percent", value: c.RootDiskPercent},
		{name: "diagnostics.resource_thresholds.gpu_util_percent", value: c.GPUUtilPercent},
	}
	for _, check := range checks {
		if err := validatePercentThreshold(check.name, check.value); err != nil {
			return err
		}
	}
	for nodeID, override := range c.Nodes {
		if !nodeIDs[nodeID] {
			return fmt.Errorf("diagnostics.resource_thresholds.nodes references missing node %s", nodeID)
		}
		nodeChecks := []struct {
			name  string
			value *float64
		}{
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.cpu_percent", nodeID), value: override.CPUPercent},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.memory_percent", nodeID), value: override.MemoryPercent},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.root_disk_percent", nodeID), value: override.RootDiskPercent},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.gpu_util_percent", nodeID), value: override.GPUUtilPercent},
		}
		for _, check := range nodeChecks {
			if err := validatePercentThreshold(check.name, check.value); err != nil {
				return err
			}
		}
	}
	positiveChecks := []struct {
		name  string
		value *float64
	}{
		{name: "diagnostics.resource_thresholds.network_rx_bps", value: c.NetworkRXBps},
		{name: "diagnostics.resource_thresholds.network_tx_bps", value: c.NetworkTXBps},
		{name: "diagnostics.resource_thresholds.storage_read_bps", value: c.StorageReadBps},
		{name: "diagnostics.resource_thresholds.storage_write_bps", value: c.StorageWriteBps},
	}
	for _, check := range positiveChecks {
		if err := validatePositiveThreshold(check.name, check.value); err != nil {
			return err
		}
	}
	for nodeID, override := range c.Nodes {
		nodePositiveChecks := []struct {
			name  string
			value *float64
		}{
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.network_rx_bps", nodeID), value: override.NetworkRXBps},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.network_tx_bps", nodeID), value: override.NetworkTXBps},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.storage_read_bps", nodeID), value: override.StorageReadBps},
			{name: fmt.Sprintf("diagnostics.resource_thresholds.nodes.%s.storage_write_bps", nodeID), value: override.StorageWriteBps},
		}
		for _, check := range nodePositiveChecks {
			if err := validatePositiveThreshold(check.name, check.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePercentThreshold(name string, value *float64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 || *value > 100 {
		return fmt.Errorf("%s must be greater than 0 and no more than 100", name)
	}
	return nil
}

func validatePositiveThreshold(name string, value *float64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf("%s must be greater than 0", name)
	}
	return nil
}
