package session

// Store tracks token usage across sessions.
type Store interface {
	GetUsage(tokenHash string) (int64, error)
	AddUsage(tokenHash string, count int64) (int64, error)
	Reset(tokenHash string) error
}
