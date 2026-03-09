package model

import "fmt"

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

func (v *VlessCreds) ToTelegramClientOutput() string {
	return fmt.Sprintf("Ваша ссылка для подлюключения готова - %s, \n информацию по подключению можете найти в /instruction", v.URI)
}

func (v *VlessCreds) GetUserID() string {
	return v.UserID
}
