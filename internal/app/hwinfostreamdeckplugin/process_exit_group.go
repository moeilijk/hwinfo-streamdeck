package hwinfostreamdeckplugin

// processExitGroup ties a child process's lifetime to the plugin process.
// A nil value means the group was not successfully created.
type processExitGroup interface {
	dispose() error
}
