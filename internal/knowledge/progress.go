package knowledge

type ProgressPhase string

const (
	ProgressCollect    ProgressPhase = "collect"
	ProgressAnalyze    ProgressPhase = "analyze"
	ProgressSynthesize ProgressPhase = "synthesize"
)

type ProgressTask struct {
	Phase    ProgressPhase
	Source   string
	Provider string
	Current  int
	Total    int
}

// ProgressReporter reports a long-running operation. Start returns a function
// that must be called once with the operation's final error, if any.
type ProgressReporter interface {
	Start(ProgressTask) func(error)
}

func (service Service) startProgress(task ProgressTask) func(error) {
	if service.Progress == nil {
		return func(error) {}
	}
	finish := service.Progress.Start(task)
	if finish == nil {
		return func(error) {}
	}
	return finish
}
