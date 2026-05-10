package model

type VlessCreds struct {
	UserID string
	URI    string
}

func (v *VlessCreds) GetLink() string {
	return v.URI
}

func (v *VlessCreds) GetUserID() string {
	return v.UserID
}
