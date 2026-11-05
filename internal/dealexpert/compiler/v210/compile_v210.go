package v210

// Compile executes only this independently versioned implementation. All model
// references are bound and materialized before final validation.
func Compile(snapshot, intermediate []byte, rulesVersion string) (Result, error) {
	return compileSnapshot(snapshot, intermediate, rulesVersion)
}
