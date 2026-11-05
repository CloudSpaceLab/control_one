package auth

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// An actual loopback LDAP exchange: bind, user search, user bind and group
// search. This tests the provider's behavior when that final search is denied.
func TestLDAPGroupSearchFailureRejectsAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name        string
		groupCode   int64
		rebindCode  int64
		wantMessage string
	}{{"group denied", 50, 0, "ldap group search"}, {"service rebind denied", 0, 49, "ldap service rebind"}, {"successful empty groups use configured default", 0, 0, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					done <- e
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				for step := 0; step < 5; step++ {
					packet, e := ber.ReadPacket(conn)
					if e != nil {
						done <- e
						return
					}
					id := packet.Children[0].Value.(int64)
					if step == 0 || step == 2 || step == 3 {
						expected := "cn=service,dc=example,dc=invalid"
						if step == 2 {
							expected = "uid=reviewer,dc=example,dc=invalid"
						}
						if packet.Children[1].Tag != 0 || packet.Children[1].Children[1].Value != expected {
							done <- fmt.Errorf("unexpected bind identity at step %d", step)
							return
						}
					} else if packet.Children[1].Tag != 3 {
						done <- fmt.Errorf("expected search at step %d", step)
						return
					}
					if step == 1 {
						msg := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "message")
						msg.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, "id"))
						entry := ber.Encode(ber.ClassApplication, ber.TypeConstructed, 4, nil, "entry")
						entry.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "uid=reviewer,dc=example,dc=invalid", "dn"))
						attrs := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "attributes")
						for _, pair := range [][2]string{{"mail", "reviewer@example.invalid"}, {"displayName", "Reviewer"}} {
							attr := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "attribute")
							attr.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, pair[0], "name"))
							values := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "values")
							values.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, pair[1], "value"))
							attr.AppendChild(values)
							attrs.AppendChild(attr)
						}
						entry.AppendChild(attrs)
						msg.AppendChild(entry)
						if _, e = conn.Write(msg.Bytes()); e != nil {
							done <- e
							return
						}
					}
					responseTag := ber.Tag(1)
					if step == 1 || step == 4 {
						responseTag = 5
					}
					result := int64(0)
					if step == 3 {
						result = tc.rebindCode
					}
					if step == 4 {
						result = tc.groupCode
					}
					msg := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "message")
					msg.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, "id"))
					response := ber.Encode(ber.ClassApplication, ber.TypeConstructed, responseTag, nil, "response")
					response.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, result, "result"))
					response.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matched"))
					response.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "diagnostic"))
					msg.AppendChild(response)
					if _, e = conn.Write(msg.Bytes()); e != nil {
						done <- e
						return
					}
					if step == 3 && tc.rebindCode != 0 {
						done <- nil
						return
					}
				}
				done <- nil
			}()
			provider, err := NewLDAPProvider(LDAPConfig{Enabled: true, URL: "ldap://" + listener.Addr().String(), BindDN: "cn=service,dc=example,dc=invalid", BindPassword: "fixture", UserBaseDN: "dc=example,dc=invalid", GroupBaseDN: "dc=example,dc=invalid", DefaultRole: "viewer"}, zap.NewNop())
			require.NoError(t, err)
			user, err := provider.Authenticate(context.Background(), "reviewer@example.invalid", "fixture")
			if tc.wantMessage != "" {
				require.ErrorContains(t, err, tc.wantMessage)
				require.Nil(t, user)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"viewer"}, user.Roles)
			}
			require.NoError(t, <-done)
		})
	}
}
