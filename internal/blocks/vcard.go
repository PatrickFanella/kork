package blocks

import "strings"

// VCard renders a vCard 3.0 for a contact block.
func VCard(c Contact) string {
	esc := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\n", `\n`, "\r", "")
	var b strings.Builder
	line := func(k, v string) {
		if v != "" {
			b.WriteString(k + ":" + esc.Replace(v) + "\r\n")
		}
	}
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	line("FN", c.Name)
	b.WriteString("N:" + esc.Replace(c.Name) + ";;;;\r\n")
	line("ORG", c.Org)
	line("TEL;TYPE=CELL", c.Phone)
	line("EMAIL", c.Email)
	line("URL", c.URL)
	line("NOTE", c.Note)
	b.WriteString("END:VCARD\r\n")
	return b.String()
}
