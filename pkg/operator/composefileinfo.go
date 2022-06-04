package operator

type Config struct {
	Version  string             `yaml:"version"`
	Networks map[string]Network `yaml:"networks"`
	Volumes  map[string]Volume  `yaml:"volumes"`
	Services map[string]Service `yaml:"services"`
}

type Network struct {
	Driver     string            `yaml:"driver"`
	External   string            `yaml:"external"`
	DriverOpts map[string]string `yaml:"driver_opts"`
}

type Volume struct {
	Driver     string            `yaml:"driver"`
	External   string            `yaml:"external"`
	DriverOpts map[string]string `yaml:"driver_opts"`
}

type Service struct {
	ContainerName string            `yaml:"container_name"`
	Restart       string            `yaml:"restart"`
	Image         string            `yaml:"image"`
	Links         []string          `yaml:"links"`
	Labels        map[string]string `yaml:"labels"`
	Volumes       []string          `yaml:"volumes"`
	Ports         []string          `yaml:"ports"`
	Environment   map[string]string `yaml:"environment"`
	Networks      []string          `yaml:"networks"`
	Command       string            `yaml:"command"`
	VolumesFrom   []string          `yaml:"volumes_from"`
	DependsOn     []string          `yaml:"depends_on"`
	CapAdd        []string          `yaml:"cap_add"`
	Build         struct{ Context, Dockerfile string }
}
