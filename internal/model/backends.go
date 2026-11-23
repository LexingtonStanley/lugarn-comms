package model

type HerdrBackend string

const (
	HerdrBackendHC    HerdrBackend = "hc"
	HerdrBackendHerdr HerdrBackend = "herdr"
)

func (HerdrBackend) Values() []HerdrBackend { return []HerdrBackend{HerdrBackendHerdr, HerdrBackendHC} }
func (v HerdrBackend) Valid() bool          { return v == HerdrBackendHC || v == HerdrBackendHerdr }

type HerdrBackendChoice string

const (
	HerdrBackendChoiceAuto  HerdrBackendChoice = "auto"
	HerdrBackendChoiceHC    HerdrBackendChoice = "hc"
	HerdrBackendChoiceHerdr HerdrBackendChoice = "herdr"
)

func (HerdrBackendChoice) Values() []HerdrBackendChoice {
	return []HerdrBackendChoice{HerdrBackendChoiceAuto, HerdrBackendChoiceHerdr, HerdrBackendChoiceHC}
}
func (v HerdrBackendChoice) Valid() bool {
	return v == HerdrBackendChoiceAuto || HerdrBackend(v).Valid()
}
