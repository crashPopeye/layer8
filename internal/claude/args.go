package claude

// Args is the argv after the binary for o.
func Args(o Options) []string {
	args := []string{
		"--output-format", "stream-json", "--verbose", "--input-format", "stream-json", "--include-partial-messages",
		"--permission-prompt-tool=stdio", "--setting-sources=user,project,local",
	}
	if o.PluginDir != "" {
		args = append(args, "--plugin-dir", o.PluginDir)
	}
	for _, d := range o.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
		if o.Fork {
			args = append(args, "--fork-session")
		}
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	return args
}
