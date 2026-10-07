package sandbox

func applyDockerRuntimeDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	// The image is deliberately not defaulted here: it is this backend's
	// template, and a config that fails to name one must be reported as
	// incomplete rather than silently pointed at whatever image the release
	// happens to ship.
	if cfg.DockerHost == "" {
		cfg.DockerHost = DetectLocalDockerHost()
	}
	if cfg.DockerCPULimit <= 0 {
		cfg.DockerCPULimit = DefaultDockerCPULimit
	}
	if cfg.DockerMemoryBytes <= 0 {
		cfg.DockerMemoryBytes = DefaultDockerMemoryLimit
	}
	if cfg.DockerPidsLimit <= 0 {
		cfg.DockerPidsLimit = DefaultDockerPidsLimit
	}
	if cfg.DockerIdleTTL <= 0 {
		cfg.DockerIdleTTL = DefaultDockerIdleTTL
	}
	if cfg.DockerHTTPTimeout <= 0 {
		cfg.DockerHTTPTimeout = DefaultDockerHTTPTimeout
	}
}
