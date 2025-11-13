package model

type VerifyCase struct {
	Rom    string   `json:"rom"`
	Reason []string `json:"reason"`
}

type VerifyLocation struct {
	Location string       `json:"location"`
	List     []VerifyCase `json:"list"`
}

type VerifyOutput struct {
	CaseList []VerifyLocation `json:"case_list"`
}
