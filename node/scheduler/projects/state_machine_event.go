package projects

import (
	"golang.org/x/xerrors"
)

type mutator interface {
	apply(state *ProjectInfo)
}

// globalMutator is an event which can apply in every state
type globalMutator interface {
	// applyGlobal applies the event to the state. If if returns true,
	//  event processing should be interrupted
	applyGlobal(state *ProjectInfo) bool
}

// Ignorable Ignorable
type Ignorable interface {
	Ignore()
}

// Global events

// ProjectRestart restarts project
type ProjectRestart struct{}

func (evt ProjectRestart) applyGlobal(state *ProjectInfo) bool {
	state.RetryCount = 0
	return false
}

// ProjectForceState forces an project state
type ProjectForceState struct {
	State   ProjectState
	Details string
}

func (evt ProjectForceState) applyGlobal(state *ProjectInfo) bool {
	state.State = evt.State
	state.RetryCount = 0
	return true
}

// InfoUpdate update project info
type InfoUpdate struct {
	Size   int64
	Blocks int64
}

func (evt InfoUpdate) applyGlobal(state *ProjectInfo) bool {
	// if state.Status == SeedPulling || state.Status == SeedUploading {
	// 	state.Size = evt.Size
	// 	state.Blocks = evt.Blocks
	// }

	return true
}

func (evt InfoUpdate) Ignore() {
}

// DeployResult represents the result of node starting
type DeployResult struct {
	BlocksCount int64
	Size        int64
}

func (evt DeployResult) apply(state *ProjectInfo) {
	// if state.Status == SeedPulling || state.Status == SeedUploading {
	// 	state.Size = evt.Size
	// 	state.Blocks = evt.BlocksCount
	// }
}

func (evt DeployResult) Ignore() {
}

// DeployRequestSent indicates that a pull request has been sent
type DeployRequestSent struct{}

func (evt DeployRequestSent) apply(state *ProjectInfo) {
}

// ProjectRedeploy re-pull the project
type ProjectRedeploy struct{}

func (evt ProjectRedeploy) apply(state *ProjectInfo) {
	state.RetryCount++
}

func (evt ProjectRedeploy) Ignore() {
}

// DeploySucceed indicates that a node has successfully pulled an project
type DeploySucceed struct{}

func (evt DeploySucceed) apply(state *ProjectInfo) {
	state.RetryCount = 0

	// Check to node offline while replenishing the temporary replicas
	// After these temporary replicas are pulled, the count should be deleted
	// if state.Status == EdgesPulling {
	// 	state.ReplenishReplicas = 0
	// }
}

func (evt DeploySucceed) Ignore() {
}

// SkipStep skips the current step
type SkipStep struct{}

func (evt SkipStep) apply(state *ProjectInfo) {}

// DeployFailed indicates that a node has failed to pull an project
type DeployFailed struct{ error }

// FormatError Format error
func (evt DeployFailed) FormatError(xerrors.Printer) (next error) { return evt.error }

func (evt DeployFailed) apply(state *ProjectInfo) {
	state.RetryCount = 1
}

func (evt DeployFailed) Ignore() {
}

// CreateFailed  indicates that node selection has failed
type CreateFailed struct{ error }

// FormatError Format error
func (evt CreateFailed) FormatError(xerrors.Printer) (next error) { return evt.error }

func (evt CreateFailed) apply(state *ProjectInfo) {
}
