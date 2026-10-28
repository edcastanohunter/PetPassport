<#macro emailLayout>
<html lang="${locale.currentLanguageTag!'es'}">
<body style="margin:0;padding:0;background:#f8fafc;font-family:Arial,Helvetica,sans-serif;color:#111827;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f8fafc;">
    <tr><td align="center" style="padding:24px 12px;">
      <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#ffffff;border:1px solid #e5e7eb;border-radius:8px;">
        <tr><td style="padding:20px 24px;border-bottom:1px solid #e5e7eb;font-size:20px;font-weight:bold;color:#1d4ed8;">PetPassport</td></tr>
        <tr><td style="padding:24px;font-size:15px;line-height:1.5;">
          <#nested>
        </td></tr>
        <tr><td style="padding:16px 24px;border-top:1px solid #e5e7eb;font-size:12px;color:#4b5563;">
          ${msg("ppEmailFooter")}
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>
</#macro>
