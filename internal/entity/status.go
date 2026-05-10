package entity

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDone       Status = "done"
	StatusError      Status = "error"
)

func (s Status) IsTerminal() bool {
	return s == StatusDone || s == StatusError
}
