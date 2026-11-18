package models

type SafeUserInfo struct {
	FullName string
	UserID   int64
}
type Credentials struct {
	CredID   string
	Password string
}

type Tokens struct {
	PublicToken  string
	PrivateToken string
}

type Student struct {
	InternalID  string // Acquired from Moodle's API, not to be confused with CredID used to authenticate.
	Credentials Credentials
	Tokens      Tokens
}
