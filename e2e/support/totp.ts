import { createHmac } from 'node:crypto'

// TOTP RFC 6238 (SHA1, 6 dígitos, 30 s) como la política del realm (IAM-005),
// para que el super_admin del E2E configure y use su OTP.
function base32(secret: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const ch of secret.replace(/[\s=]/g, '').toUpperCase()) {
    const v = alphabet.indexOf(ch)
    if (v < 0) throw new Error(`base32 inválido: ${ch}`)
    bits += v.toString(2).padStart(5, '0')
  }
  return Buffer.from((bits.match(/.{8}/g) ?? []).map((b) => parseInt(b, 2)))
}

export function totp(secret: string, now = Date.now()): string {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(now / 1000 / 30)))
  const hmac = createHmac('sha1', base32(secret)).update(counter).digest()
  const offset = hmac[hmac.length - 1]! & 0x0f
  const code = (hmac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000
  return code.toString().padStart(6, '0')
}
