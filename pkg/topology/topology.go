package topology

import "gopkg.in/yaml.v2"

type ConfigYaml struct {
	Hosts []Host `yaml:"hosts"`
}

type Host struct {
	Name  string `yaml:"name"`
	Rails []Rail `yaml:"rails"`
}

type Rail struct {
	Tor    string `yaml:"tor"`
	Local  string `yaml:"local"`
	Subnet string `yaml:"subnet"`
}

// config example
// var cfg = `
// hosts:
//   - name: host1
//     rails:
//       - tor: 2.0.0.1
//         local: 2.0.0.2
//   - name: host2
//     rails:
//       - tor: 2.0.0.1
//         local: 2.0.0.3
// `

func LoadConfig(input string) (*ConfigYaml, error) {
	cfg := &ConfigYaml{}

	// unmarshal into an internface map in order to get only what is needed,
	// otherwise unmarshal will fail
	var intermediate map[string]interface{}
	err := yaml.Unmarshal([]byte(input), &intermediate)
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(intermediate) // Remarshal cleaned data
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

type Config map[string]Host

func LoadConfigMap(input string) (Config, error) {
	cfgyaml, err := LoadConfig(input)
	if err != nil {
		return nil, err
	}

	config := make(Config)

	for i := range cfgyaml.Hosts {
		config[cfgyaml.Hosts[i].Name] = cfgyaml.Hosts[i]
	}

	return config, nil
}
