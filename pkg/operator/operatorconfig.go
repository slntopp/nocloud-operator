package operator

type OperatorConfig struct {
	Duration      int    `yaml:"duration"`
	ComposePrefix string `yaml:"composePrefix"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	ServerAddress string `yaml:"serverAddress"`
}
