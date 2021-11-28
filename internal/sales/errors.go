package sales

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrNotFound Error = "sale record(s) not found"
)
