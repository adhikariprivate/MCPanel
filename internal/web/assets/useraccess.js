(() => {
  const preset = document.getElementById("access-preset");
  if (!preset) return;

  const sets = {
    "read-only": ["files.read", "vhost.view", "ssl.view", "cron.view", "logs.view", "database.view", "backup.view", "email.view", "wordpress.view", "cloud.view"],
    developer: ["site.settings", "files.read", "files.write", "vhost.view", "ssl.view", "cron.view", "cron.manage", "logs.view", "database.view", "backup.view", "email.view", "wordpress.view"],
    "site-manager": ["site.settings", "vhost.view", "vhost.manage", "files.read", "files.write", "files.delete", "files.permissions", "ssl.view", "ssl.manage", "cron.view", "cron.manage", "logs.view", "database.view", "backup.view", "email.view", "email.manage", "wordpress.view", "wordpress.manage", "cloud.view", "cloud.manage", "app.manage"],
  };

  preset.addEventListener("change", () => {
    const allowed = new Set(sets[preset.value] || []);
    document.querySelectorAll(".grant-select").forEach((select) => {
      select.value = preset.value ? (allowed.has(select.dataset.capability) ? "allow" : "deny") : "";
    });
  });
})();
