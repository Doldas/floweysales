package identity

import "context"

type SMTPSettings struct {
 Host string `json:"host"`
 Port int `json:"port"`
 Sender string `json:"sender"`
 Username string `json:"username"`
 PublicURL string `json:"public_url"`
}

func (s *Service) GetSMTPSettings(ctx context.Context)(SMTPSettings,error){
 var cfg SMTPSettings
 err:=s.db.QueryRowContext(ctx,"SELECT host,port,sender,username,public_url FROM smtp_configuration WHERE id=1").Scan(&cfg.Host,&cfg.Port,&cfg.Sender,&cfg.Username,&cfg.PublicURL)
 return cfg,err
}
func (s *Service) SaveSMTPSettings(ctx context.Context,cfg SMTPSettings)error{
 _,err:=s.db.ExecContext(ctx,`INSERT INTO smtp_configuration(id,host,port,sender,username,public_url) VALUES(1,?,?,?,?,?)
 ON DUPLICATE KEY UPDATE host=VALUES(host),port=VALUES(port),sender=VALUES(sender),username=VALUES(username),public_url=VALUES(public_url)`,cfg.Host,cfg.Port,cfg.Sender,cfg.Username,cfg.PublicURL)
 return err
}
