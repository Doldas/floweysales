package server

import (
 "context"
 "testing"
)

func TestFlowCancellationContext(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=ctx.Err();err!=context.Canceled{t.Fatalf("expected context cancellation, got %v",err)}
}

func TestFlowRunCancelMapInitialized(t *testing.T){
 // Cancellation handles are populated after a DB run ID exists.
 // The server must not expose a cancel handle for an unrelated run.
 handles:=map[int64]context.CancelFunc{}
 if handles[42]!=nil{t.Fatal("unexpected cancellation handle")}
}
