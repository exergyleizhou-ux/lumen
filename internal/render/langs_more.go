package render

func init() {
	RegisterLang(&Lang{
		Line:       "//",
		BlockOpen:  "/*",
		BlockClose: "*/",
		Keywords: set(
			"fn", "let", "mut", "const", "struct", "enum", "impl", "trait",
			"pub", "use", "mod", "match", "if", "else", "for", "while", "loop",
			"return", "self", "Self", "true", "false", "Some", "None", "Ok", "Err",
			"as", "in", "ref", "move", "where", "type", "static", "dyn", "unsafe",
			"extern", "crate", "super", "break", "continue", "async", "await",
		),
	}, "rust", "rs")

	RegisterLang(&Lang{
		Line:       "//",
		BlockOpen:  "/*",
		BlockClose: "*/",
		Keywords: set(
			"class", "interface", "public", "private", "protected", "static",
			"final", "void", "int", "long", "double", "boolean", "new", "return",
			"if", "else", "for", "while", "switch", "case", "break", "this",
			"null", "true", "false", "import", "package", "extends", "implements",
			"try", "catch", "throw", "throws", "abstract", "enum", "super",
		),
	}, "java")

	RegisterLang(&Lang{
		Line: "#",
		Keywords: set(
			"def", "end", "class", "module", "if", "elsif", "else", "unless",
			"while", "do", "return", "yield", "self", "nil", "true", "false",
			"require", "attr_accessor", "attr_reader", "attr_writer", "include",
			"extend", "raise", "rescue", "ensure", "begin", "case", "when",
			"then", "next", "break",
		),
	}, "ruby", "rb")

	RegisterLang(&Lang{
		Line:       "--",
		BlockOpen:  "/*",
		BlockClose: "*/",
		Keywords: set(
			"select", "SELECT", "from", "FROM", "where", "WHERE",
			"insert", "INSERT", "update", "UPDATE", "delete", "DELETE",
			"create", "CREATE", "table", "TABLE", "join", "JOIN", "on", "ON",
			"group", "GROUP", "by", "BY", "order", "ORDER", "limit", "LIMIT",
			"and", "AND", "or", "OR", "not", "NOT", "null", "NULL",
			"values", "VALUES", "into", "INTO", "set", "SET",
		),
	}, "sql")

	RegisterLang(&Lang{
		Line:     "#",
		Keywords: set("true", "false"),
	}, "toml")

	RegisterLang(&Lang{
		Line:     "#",
		Keywords: set("true", "false", "null", "yes", "no", "on", "off"),
	}, "yaml", "yml")

	RegisterLang(&Lang{}, "html", "htm", "xml")
}

func set(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}
