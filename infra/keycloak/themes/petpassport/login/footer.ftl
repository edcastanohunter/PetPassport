<#-- Pie de todas las páginas de login (IAM-012): términos y privacidad de la app (SEC-009). -->
<#macro content>
<#if client?? && client.baseUrl?has_content>
  <nav class="pp-legal" aria-label="${msg("ppLegalNav")}">
    <a href="${client.baseUrl?remove_ending("/")}/legal/terms">${msg("ppTerms")}</a>
    <a href="${client.baseUrl?remove_ending("/")}/legal/privacy">${msg("ppPrivacy")}</a>
  </nav>
</#if>
</#macro>
