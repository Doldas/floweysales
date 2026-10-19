package sec
import(
 "strings"
 "testing"
)
func TestFilingSectionExtraction(t *testing.T){
 raw:=[]byte(`<html><head><style>private</style></head><body><p>TABLE OF CONTENTS</p>
 <p>Item 7. Management Discussion</p><p>Item 8. Accounts</p>
 <h2>Item 7. Management's Discussion and Analysis</h2>
 <p>Operating segments generated important revenues, cash flow and financial statements.</p>
 <p>Our CFO improved for five quarters; repeated information should not be guessed.</p>
 <h2>Item 7A. Market Risks</h2><p>Foreign exchange.</p></body></html>`)
 text:=cleanFilingHTML(raw)
 snippet,truncated,err:=extractItem(text,"7",12000)
 if err!=nil{t.Fatal(err)}
 if truncated||!strings.Contains(snippet,"Our CFO improved")||strings.Contains(snippet,"Foreign exchange"){t.Fatalf("wrong excerpt %q",snippet)}
 if !allowedItem("10-K","7") || allowedItem("10-Q","7") || allowedItem("8-K","3"){t.Fatal("invalid form item policy")}
 if _,_,err:=extractItem(text,"9A",12000);err==nil{t.Fatal("missing SEC section must not hallucinate")}
}
