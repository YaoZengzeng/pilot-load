package util

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func AddError(e1, e2 error) error {
	if e1 == nil {
		return e2
	}
	if e2 == nil {
		return e1
	}
	return fmt.Errorf("%v and %v", e1, e2)
}

func StringPointer(s string) *string {
	return &s
}

func StringDefault(s string, def string) string {
	if s != "" {
		return s
	}
	return def
}

func BoolPointer(b bool) *bool {
	return &b
}

func IsDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func ContextSleep(ctx context.Context, dur time.Duration) {
	select {
	case <-time.After(dur):
	case <-ctx.Done():
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func GenUID() string {
	parts := strings.Split(uuid.New().String(), "-")
	return parts[0]
}

var (
	ipMutex sync.Mutex
	// This is to prevent multi pilot load generate pods with same ip.
	nextIp = net.ParseIP(fmt.Sprintf("%d.%d.0.10", (uuid.New().ID()%255)+1, uuid.New().ID()%255))
)

func GetIP() string {
	ipMutex.Lock()
	defer ipMutex.Unlock()
	i := nextIp.To4()
	ret := i.String()
	v := uint(i[0])<<24 + uint(i[1])<<16 + uint(i[2])<<8 + uint(i[3])
	v += 1
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	nextIp = net.IPv4(v0, v1, v2, v3)
	return ret
}
