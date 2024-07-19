package metrics

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type PrometheusConfig struct {
	ScrapeConfigs []ScrapeConfig `yaml:"scrape_configs"`
}

type ScrapeConfig struct {
	JobName       string         `yaml:"job_name"`
	StaticConfigs []StaticConfig `yaml:"static_configs"`
}

type StaticConfig struct {
	Targets []string `yaml:"targets"`
}

func readPortFromConfigFile(name string) (uint, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return 0, fmt.Errorf("could not read file %s: %w", name, err)
	}

	var config PrometheusConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return 0, fmt.Errorf("could not parse YAML from %s: %w", name, err)
	}

	if len(config.ScrapeConfigs) == 0 {
		return 0, fmt.Errorf("no scrape configs found in %s", name)
	}

	for _, scrapeConfig := range config.ScrapeConfigs {
		if len(scrapeConfig.StaticConfigs) == 0 {
			return 0, fmt.Errorf("no static configs found for job %s in %s", scrapeConfig.JobName, name)
		}

		for j, staticConfig := range scrapeConfig.StaticConfigs {
			if len(staticConfig.Targets) == 0 {
				return 0, fmt.Errorf("no targets found for job %s, static config %d in %s", scrapeConfig.JobName, j, name)
			}

			for k, target := range staticConfig.Targets {
				port, err := extractPortFromTarget(target)
				if err != nil {
					return 0, fmt.Errorf("invalid target in job %s, static config %d, target %d: %w", scrapeConfig.JobName, j, k, err)
				}
				return port, nil
			}
		}
	}

	return 0, fmt.Errorf("no valid targets found in %s", name)
}

func extractPortFromTarget(target string) (uint, error) {
	parts := strings.Split(target, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid target format: %s", target)
	}

	port, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid port number: %s", parts[1])
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port number out of range (1-65535): %d", port)
	}

	return uint(port), nil
}
