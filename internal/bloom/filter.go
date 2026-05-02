package bloom

type Filter struct {
	expectedItems uint64
	falsePositive float64
}

func New(expectedItems uint64, falsePositive float64) *Filter {
	return &Filter{
		expectedItems: expectedItems,
		falsePositive: falsePositive,
	}
}

func (f *Filter) Add(key string) {}

func (f *Filter) MightContain(key string) bool {
	return false
}
