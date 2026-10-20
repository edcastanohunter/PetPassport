import { InMemoryWebStorage, UserManager, WebStorageStateStore } from 'oidc-client-ts'
import type { RuntimeConfig } from '@/shared/config/runtime'

// UserManager de oidc-client-ts según docs/phase-1/07 §2 (FE-002): Authorization
// Code + PKCE, tokens solo en memoria (nunca localStorage), renovación silenciosa
// por refresh token y sin iframe de sesión (monitorSession=false).
export function createUserManager(config: RuntimeConfig, origin = window.location.origin) {
  return new UserManager({
    authority: config.oidc.issuer,
    client_id: config.oidc.clientId,
    redirect_uri: `${origin}/auth/callback`,
    post_logout_redirect_uri: `${origin}/auth/logout`,
    response_type: 'code',
    scope: 'openid profile email',
    automaticSilentRenew: true,
    monitorSession: false,
    loadUserInfo: false,
    // El estado del flujo (state/PKCE verifier) sí necesita sobrevivir al redirect:
    // va a sessionStorage (no contiene tokens).
    stateStore: new WebStorageStateStore({ store: window.sessionStorage }),
    userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
  })
}

export type AppUserManager = ReturnType<typeof createUserManager>
