package model

type VlessCreds struct {
	UserID string

	UUID     string
	Host     string
	Port     uint32
	Security string
	Sni      string
	Alpn     string
	Path     string
	Network  string
	Flow     string
	URI      string
}

func (v *VlessCreds) GetLink() string {
	return v.URI
}

func (v *VlessCreds) GetUserID() string {
	return v.UserID
}
