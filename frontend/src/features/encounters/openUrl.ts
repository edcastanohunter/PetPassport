/**
 * Abre una URL prefirmada en otra pestaña. Se usa un `<a>` y no
 * `window.open(…, 'noopener')`, que devuelve null y no permite detectar el
 * bloqueo; se invoca dentro de la activación del clic del usuario.
 */
export function openUrl(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.target = '_blank'
  a.rel = 'noopener noreferrer'
  document.body.appendChild(a)
  a.click()
  a.remove()
}
