package wecom

import "errors"

var (
	ErrSuiteTicketMissing     = errors.New("wecom suite ticket missing")
	ErrCorpInstallMissing     = errors.New("wecom corp install missing")
	ErrCorpInstallContextMiss = errors.New("wecom corp install context missing")
)
