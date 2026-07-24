package dispatch

// Service is the TUI-facing facade. Config is loaded once at construction;
// detection runs per call so environment changes are reflected.
type Service struct {
	cfg Config
	env Env
}

func NewService(configPath string) (*Service, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, env: OSEnv()}, nil
}

// Preview describes what Spawn would do, e.g. "tmux · new window".
func (s *Service) Preview() (string, error) {
	t, err := Detect(s.cfg, s.env, "")
	if err != nil {
		return "", err
	}
	return t.Describe(), nil
}

// PreviewTarget describes the target that would be used for the given forced
// target name, e.g. "herdr · new pane" or "terminal · kitty".
func (s *Service) PreviewTarget(target string) (string, error) {
	t, err := Detect(s.cfg, s.env, target)
	if err != nil {
		return "", err
	}
	return t.Describe(), nil
}

func (s *Service) Spawn(spec Spec) error {
	t, err := Detect(s.cfg, s.env, spec.Target)
	if err != nil {
		return err
	}
	return t.Spawn(spec)
}
