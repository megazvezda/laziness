package models

type Student struct {
	credId     string
	internalId string
	Credentials
}

type Credentials struct {
	password     string
	publicToken  string
	privateToken string
}
