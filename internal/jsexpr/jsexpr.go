// Package jsexpr holds the forms Page.Eval runs code in, and tells
// JavaScript that cannot be an expression from its tokens, without parsing
// it. Page.Eval runs such code as a function body rather than trying it as
// an expression first: WebKit tells the page's error event of a script
// that does not compile.
package jsexpr

import "strings"

// The forms of Page.Eval, fmt formats of code (%s) run as the body of an
// async function: as an expression, which it tries first (a compile error
// means code is not one), and as a function body.
const (
	ExprForm = "try{return JSON.stringify({ok:true,v:await(\n%s\n)})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
	BodyForm = "try{return JSON.stringify({ok:true,v:await(async()=>{\n%s\n})()})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
)

// Expr returns code as Page.Eval puts it in ExprForm: without the spaces
// and semicolons that end it.
func Expr(code string) string { return strings.TrimRight(code, " \t\r\n;") }

// What the last token leaves the next one to be, which tells a regular
// expression from a division.
const (
	expectOperand = iota // at the start and after operators: "/" begins a regular expression
	operand              // after a name, a literal, ")" or "]": "/" divides
	unsure               // after "}", "++", "--", async, await, of, yield: either
	property             // after "." and "?.": a property's name, even a keyword
)

// NotExpression reports whether code cannot be a JavaScript expression:
// outside its brackets it has a ";", a word only statements have (const,
// if, return, …), or two operands in a row, as statements on lines of
// their own without semicolons do ("a()\nb()").
//
// False means it may be one. NotExpression reads tokens alone, so it
// answers false where they depend on more than the token before: a "/"
// after "}", "++", "--", async, await, of or yield, which may divide or
// begin a regular expression, and "<!--" and "-->", which may begin a
// comment. It answers false too for non-ASCII characters, "\", "#" and "@"
// outside strings, templates, regular expressions and comments.
func NotExpression(code string) bool {
	var open []byte // what is open: ( [ {, h for a statement's head, $ for a template's ${
	prev := expectOperand
	head := false // the last word was if, for, while or with
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			i++
			continue
		case c == '/' && strings.HasPrefix(code[i:], "//"):
			if j := strings.IndexAny(code[i:], "\n\r\u2028\u2029"); j >= 0 {
				i += j
			} else {
				i = len(code)
			}
			continue
		case c == '/' && strings.HasPrefix(code[i:], "/*"):
			j := strings.Index(code[i+2:], "*/")
			if j < 0 {
				return false
			}
			i += j + 4
			continue
		}
		depth0 := len(open) == 0
		wasHead := head
		head = false
		next, starts := expectOperand, false // starts: the token begins an operand
		switch {
		case isNameStart(c):
			j := i + 1
			for j < len(code) && (isNameStart(code[j]) || isDigit(code[j])) {
				j++
			}
			word := code[i:j]
			i = j
			if prev == property {
				next = operand
				break
			}
			switch word {
			// Words only statements have. throw and do are among them
			// while the throw expressions and do expressions proposals
			// have not shipped: in an engine that ships one, "a ?? throw
			// b" or "do { … }" would compile as an expression, and Eval,
			// running it as a function body, would quietly return null.
			case "break", "case", "catch", "const", "continue", "debugger", "default", "do", "else", "export",
				"finally", "for", "if", "return", "switch", "throw", "try", "var", "while", "with":
				if depth0 {
					return true
				}
				// A head in parentheses follows these.
				head = word == "for" || word == "if" || word == "while" || word == "with"
			case "in", "instanceof", "extends":
			case "new", "typeof", "void", "delete", "function", "class", "import":
				starts = true
			case "async", "await", "of", "yield":
				next, starts = unsure, true
				head = word == "await" && wasHead // for await (
			default:
				next, starts = operand, true
			}
		case isDigit(c) || c == '.' && i+1 < len(code) && isDigit(code[i+1]):
			i = number(code, i)
			next, starts = operand, true
		case c == '"' || c == '\'':
			j := i + 1
			for ; j < len(code) && code[j] != c; j++ {
				switch code[j] {
				case '\\':
					j++
				case '\n', '\r':
					return false
				}
			}
			if j >= len(code) {
				return false
			}
			i = j + 1
			next, starts = operand, true
		case c == '`':
			j, sub := template(code, i+1)
			if j < 0 {
				return false
			}
			i, next = j, operand
			if sub {
				open, next = append(open, '$'), expectOperand
			}
		case c == '(' || c == '[' || c == '{':
			if c == '(' && wasHead {
				c = 'h'
			}
			open = append(open, c)
			i++
		case c == ')' || c == ']' || c == '}':
			if depth0 {
				return false
			}
			o := open[len(open)-1]
			open = open[:len(open)-1]
			switch {
			case c == '}' && o == '$':
				j, sub := template(code, i+1)
				if j < 0 {
					return false
				}
				i, next = j, operand
				if sub {
					open, next = append(open, '$'), expectOperand
				}
			case c == '}' && o == '{':
				i, next = i+1, unsure
			case c == ']' && o == '[', c == ')' && o == '(':
				i, next = i+1, operand
			case c == ')' && o == 'h':
				i++
			default:
				return false
			}
		case c == '/':
			switch prev {
			case operand:
				i++
			case expectOperand:
				j := regExp(code, i+1)
				if j < 0 {
					return false
				}
				i, next = j, operand
			default:
				return false
			}
		case c == ';':
			if depth0 {
				return true
			}
			i++
		case c == '?' && strings.HasPrefix(code[i:], "?.") && !(i+2 < len(code) && isDigit(code[i+2])):
			i, next = i+2, property
		case c == '.':
			if strings.HasPrefix(code[i:], "...") {
				i += 3
			} else {
				i, next = i+1, property
			}
		case c == '+' || c == '-':
			if strings.HasPrefix(code[i:], "-->") {
				return false
			}
			if i+1 < len(code) && code[i+1] == c {
				i, next = i+2, unsure
			} else {
				i++
			}
		case c == '<' && strings.HasPrefix(code[i:], "<!--"):
			return false
		case strings.IndexByte(",:?~!=<>*%&|^", c) >= 0:
			i++
		default: // non-ASCII, \, #, @, controls
			return false
		}
		if depth0 && starts && prev == operand {
			return true
		}
		prev = next
	}
	return false
}

// template reads a template literal from code[i:], after its "`" or a
// substitution's "}", up to its end or its next "${", whose ends it
// returns, with whether it is a "${"; -1 if it does not end.
func template(code string, i int) (int, bool) {
	for ; i < len(code); i++ {
		switch code[i] {
		case '\\':
			i++
		case '`':
			return i + 1, false
		case '$':
			if i+1 < len(code) && code[i+1] == '{' {
				return i + 2, true
			}
		}
	}
	return -1, false
}

// number reads a numeric literal from code[i:] and returns its end. Its
// letters run on into a name that follows, which is no JavaScript anyway.
func number(code string, i int) int {
	radix := code[i] == '0' && i+1 < len(code) && strings.IndexByte("xXoObB", code[i+1]) >= 0
	dot := code[i] == '.'
	for i++; i < len(code); i++ {
		switch c := code[i]; {
		case isNameStart(c) || isDigit(c):
			// No fraction after an exponent or a BigInt's n: 1e3.x
			// reads x of 1e3.
			dot = dot || !radix && (c|0x20 == 'e' || c == 'n')
		case c == '.' && !dot && !radix:
			dot = true
		case (c == '+' || c == '-') && !radix && code[i-1]|0x20 == 'e':
		default:
			return i
		}
	}
	return i
}

// regExp reads a regular expression literal from code[i:], after its
// "/", and returns its end, after its flags; -1 if it does not end.
func regExp(code string, i int) int {
	class := false
	for ; i < len(code); i++ {
		switch code[i] {
		case '\\':
			i++
		case '\n', '\r':
			return -1
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				i++
				for i < len(code) && (isNameStart(code[i]) || isDigit(code[i])) {
					i++
				}
				return i
			}
		}
	}
	return -1
}

func isNameStart(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_' || c == '$'
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
