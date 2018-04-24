// telesock - Fast and simple SOCKS5 proxy.
// Written in 2018 by Alexey Palazhchenko.
//
// To the extent possible under law, the author(s) have dedicated all copyright and related and neighboring rights
// to this software to the public domain worldwide. This software is distributed without any warranty.
//
// You should have received a copy of the CC0 Public Domain Dedication along with this software.
// If not, see <http://creativecommons.org/publicdomain/zero/1.0/>.

package internal

// Config represents Telesock configuration.
type Config struct {
	Server string
	Users  []struct {
		Username string
		Password string
	}
}
