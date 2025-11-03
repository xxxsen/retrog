package app

import "context"

// VerifyCommand performs duplicate detection and stores the result.
type VerifyCommand struct {
	rootDir string
	result  *VerifyResult
}

// NewVerifyCommand constructs a verify command instance.
func NewVerifyCommand(root string) *VerifyCommand {
	return &VerifyCommand{rootDir: root}
}

// Run executes the verify command.
func (c *VerifyCommand) Run(ctx context.Context) error {
	res, err := Verify(ctx, c.rootDir)
	if err != nil {
		return err
	}
	c.result = res
	return nil
}

// Result returns the verification outcome gathered during Run.
func (c *VerifyCommand) Result() *VerifyResult {
	return c.result
}
