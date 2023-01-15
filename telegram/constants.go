package telegram

const (
	DefaultHandlerGroup int = iota
	DispatcherForwardHandlerGroup
	DispatcherCallbackHandlerGroup
	ModulesStartingHandlerGroup
)
