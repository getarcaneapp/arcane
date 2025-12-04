package env

type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Summary struct {
	Variables []Variable `json:"variables"`
}
