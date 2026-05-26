package domain

type OwnedProcessRegistryRepository interface {
	OwnedPIDs() map[int]struct{}
}
