package catalog

// BlockType is a kind of content a page can contain.
type BlockType struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var BlockTypes = []BlockType{
	{"service", "Social, payment or contact", "Pick from Instagram, Cash App, PayPal, Bitcoin, WhatsApp and more"},
	{"link", "Link", "Any web, email or phone link"},
	{"heading", "Heading", "A section title"},
	{"text", "Text", "A short paragraph"},
	{"image", "Image", "An uploaded image, optionally linked"},
	{"embed", "Embed", "YouTube, Vimeo, Spotify, SoundCloud, Twitch or Apple Music player"},
	{"qr", "QR code", "A scannable code for any text or URL"},
	{"copy", "Copy button", "A value visitors can copy, like a username or address"},
	{"contact", "Contact card", "A Save contact button that downloads a vCard"},
	{"map", "Location", "Opens an address in maps"},
	{"email_signup", "Email sign-up", "Collect confirmed email addresses, kept here or sent to your Brevo list"},
	{"events", "Events", "Tour dates and shows with ticket links; past dates hide themselves"},
	{"latest", "Latest video or post", "The newest upload from a YouTube channel or any RSS/Atom feed"},
	{"divider", "Divider", "A horizontal rule"},
}

// IsBlockType reports whether key is a known block type.
func IsBlockType(key string) bool {
	for _, b := range BlockTypes {
		if b.Key == key {
			return true
		}
	}
	return false
}

var EmbedProviders = []string{"YouTube", "Vimeo", "Spotify", "SoundCloud", "Twitch", "Apple Music"}
