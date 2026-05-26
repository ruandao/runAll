package domain

type StartupSessionRepository interface {
	FindByID(sessionID string) (StartupSession, error)
	Save(session StartupSession) error
	ListRecent(limit int) ([]StartupSession, error)
}

type ServiceStartupAttemptRepository interface {
	Save(attempt ServiceStartupAttempt) error
	ListBySession(sessionID string) ([]ServiceStartupAttempt, error)
	ListRecentByService(serviceName string, limit int) ([]ServiceStartupAttempt, error)
}
