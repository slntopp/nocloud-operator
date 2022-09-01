package operator

type Registries struct {
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	ServerAddress string `yaml:"serverAddress"`
}

type OperatorConfig struct {
	Duration         int        `yaml:"duration"`
	ComposePrefix    string     `yaml:"composePrefix"`
	DockerRegistries Registries `yaml:"registries"`
	Dns              []string   `yaml:"dns"`
}
