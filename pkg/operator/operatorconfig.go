package operator

type Registries struct {
	Username      string `yaml:"username" json:"username"`
	Password      string `yaml:"password" json:"password"`
	ServerAddress string `yaml:"serverAddress" json:"server_address"`
}

type OperatorConfig struct {
	Duration         int          `yaml:"duration"`
	ComposePrefix    string       `yaml:"composePrefix"`
	DockerRegistries []Registries `yaml:"registries"`
	Dns              []string     `yaml:"dns"`
}
