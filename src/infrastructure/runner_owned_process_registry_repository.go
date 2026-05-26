package infrastructure

type RunnerOwnedProcessRegistryRepository struct {
	ownedFn func() map[int]struct{}
}

func NewRunnerOwnedProcessRegistryRepository(ownedFn func() map[int]struct{}) RunnerOwnedProcessRegistryRepository {
	return RunnerOwnedProcessRegistryRepository{ownedFn: ownedFn}
}

func (r RunnerOwnedProcessRegistryRepository) OwnedPIDs() map[int]struct{} {
	if r.ownedFn == nil {
		return map[int]struct{}{}
	}
	return r.ownedFn()
}
