package server

import (
 "encoding/base64"
 "strings"
 "testing"
)

func TestIntegrationSecretEncryption(t *testing.T) {
 t.Setenv(apiSecretEnv,base64.StdEncoding.EncodeToString([]byte(strings.Repeat("z",32))))
 ciphertext,err:=sealAPISecret("ai","sk-example-test-key")
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(ciphertext),"sk-example-test-key"){t.Fatal("secret persisted as plaintext")}
 value,err:=openAPISecret("ai",ciphertext)
 if err!=nil||value!="sk-example-test-key"{t.Fatalf("decrypt: %q, %v",value,err)}
 if _,err:=openAPISecret("web_search",ciphertext);err==nil{t.Fatal("cross-integration decryption should fail")}
 if _,err:=openAPISecret("ai",ciphertext[:10]);err==nil{t.Fatal("truncated encrypted value accepted")}
 second,err:=sealAPISecret("ai","sk-example-test-key")
 if err!=nil{t.Fatal(err)}
 if string(ciphertext)==string(second){t.Fatal("nonces must not be reused")}
}

func TestIntegrationSecretsRequireEncryptionKey(t *testing.T) {
 t.Setenv(apiSecretEnv,"")
 if _,err:=sealAPISecret("ai","some-secret");err==nil{t.Fatal("missing master key must reject secret storage")}
 t.Setenv(apiSecretEnv,base64.StdEncoding.EncodeToString([]byte("too short")))
 if _,err:=apiSecretCipher();err==nil{t.Fatal("weak master key accepted")}
}
