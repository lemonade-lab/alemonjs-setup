// 轻量零依赖语法高亮器。覆盖 JS/TS 类语言的关键字、字符串、注释、数字、
// 函数调用和标签，足以应对 AlemonJS（TS/TSX）项目的代码展示。流式场景下
// 每次重高亮开销小，优于需要异步加载的 Shiki。

const KEYWORDS = new Set([
  'const', 'let', 'var', 'function', 'return', 'if', 'else', 'for', 'while',
  'do', 'switch', 'case', 'break', 'continue', 'new', 'delete', 'typeof',
  'instanceof', 'in', 'of', 'class', 'extends', 'super', 'this', 'import',
  'export', 'from', 'default', 'async', 'await', 'yield', 'try', 'catch',
  'finally', 'throw', 'interface', 'type', 'enum', 'namespace', 'declare',
  'public', 'private', 'protected', 'readonly', 'static', 'get', 'set', 'as',
  'is', 'keyof', 'never', 'unknown', 'any', 'string', 'number', 'boolean',
  'void', 'null', 'undefined', 'true', 'false', 'abstract', 'implements',
  'package', 'require', 'module', 'with'
])

// tokenize 把代码拆成 token 序列，每个 token 有类型和文本。
type TokenKind = 'keyword' | 'string' | 'comment' | 'number' | 'function' | 'plain'

function tokenize(code: string): Array<{ kind: TokenKind; text: string }> {
  const tokens: Array<{ kind: TokenKind; text: string }> = []
  let index = 0
  const length = code.length

  const push = (kind: TokenKind, text: string) => {
    if (!text) return
    tokens.push({ kind, text })
  }

  while (index < length) {
    const char = code[index]

    // 行注释 //
    if (char === '/' && code[index + 1] === '/') {
      const start = index
      index += 2
      while (index < length && code[index] !== '\n') index++
      push('comment', code.slice(start, index))
      continue
    }
    // 块注释 /* */
    if (char === '/' && code[index + 1] === '*') {
      const start = index
      index += 2
      while (index < length && !(code[index] === '*' && code[index + 1] === '/')) {
        index++
      }
      if (index < length) index += 2
      push('comment', code.slice(start, index))
      continue
    }
    // 字符串
    if (char === '"' || char === "'" || char === '`') {
      const quote = char
      const start = index
      index++
      while (index < length) {
        if (code[index] === '\\') {
          index += 2
          continue
        }
        if (code[index] === quote) {
          index++
          break
        }
        if (quote !== '`' && code[index] === '\n') break
        index++
      }
      push('string', code.slice(start, index))
      continue
    }
    // 数字
    if (/[0-9]/.test(char) || (char === '.' && /[0-9]/.test(code[index + 1] ?? ''))) {
      const start = index
      if (char === '0' && (code[index + 1] === 'x' || code[index + 1] === 'X')) {
        index += 2
        while (/[0-9a-fA-F]/.test(code[index] ?? '')) index++
      } else {
        while (/[0-9._]/.test(code[index] ?? '')) index++
        if (code[index] === 'e' || code[index] === 'E') {
          index++
          if (code[index] === '+' || code[index] === '-') index++
          while (/[0-9]/.test(code[index] ?? '')) index++
        }
      }
      push('number', code.slice(start, index))
      continue
    }
    // 标识符
    if (/[a-zA-Z_$]/.test(char)) {
      const start = index
      while (/[a-zA-Z0-9_$]/.test(code[index] ?? '')) index++
      const word = code.slice(start, index)
      // 函数调用：标识符后紧跟 (（跳过空白）
      let lookahead = index
      while (/\s/.test(code[lookahead] ?? '')) lookahead++
      if (code[lookahead] === '(') {
        push('function', word)
      } else if (KEYWORDS.has(word)) {
        push('keyword', word)
      } else {
        push('plain', word)
      }
      continue
    }
    // 空白和其他
    push('plain', char)
    index++
  }
  return tokens
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

// highlightCode 返回带 token span 的 HTML。language 决定是否高亮关键字。
export function highlightCode(code: string, language: string): string {
  const tokens = tokenize(code)
  const enabled = Boolean(language && !/^(plain|text|txt|)$/i.test(language))
  return tokens
    .map(token => {
      const escaped = escapeHtml(token.text)
      if (!enabled) return escaped
      switch (token.kind) {
        case 'keyword':
          return `<span class="tok-keyword">${escaped}</span>`
        case 'string':
          return `<span class="tok-string">${escaped}</span>`
        case 'comment':
          return `<span class="tok-comment">${escaped}</span>`
        case 'number':
          return `<span class="tok-number">${escaped}</span>`
        case 'function':
          return `<span class="tok-function">${escaped}</span>`
        default:
          return escaped
      }
    })
    .join('')
}
