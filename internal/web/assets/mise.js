(() => {
  const forms = document.querySelectorAll("[data-mise-task-form]");
  for (const form of forms) {
    const select = form.querySelector("[data-mise-task-select]");
    const state = form.querySelector("[data-mise-state]");
    const command = form.querySelector("[data-mise-command]");
    const save = form.querySelector("[data-mise-save]");
    const discard = form.querySelector("[data-mise-discard]");
    const unsaved = form.querySelector("[data-mise-unsaved]");
    if (!select || !state || !command || !save || !discard || !unsaved) continue;

    const active = form.dataset.active || "";
    const activeLabel = form.dataset.activeLabel || "Active";
    const stoppedLabel = form.dataset.stoppedLabel || "Stopped";
    const pendingLabel = form.dataset.pendingLabel || "Pending save";
    const changedLabel = form.dataset.pendingLabel || "Pending save";

    const render = () => {
      const task = select.value;
      const changed = task !== active;
      const label = changed ? pendingLabel : (active ? activeLabel : stoppedLabel);
      state.textContent = label;
      state.className = changed ? "badge-amber" : (active ? "badge-teal" : "badge-gray");
      unsaved.textContent = changed ? changedLabel : (form.dataset.cleanLabel || "No unsaved changes");
      command.textContent = task ? `${form.dataset.commandPrefix || "mise run "}${task}` : (form.dataset.stoppedCommand || "Service will be stopped");
      save.disabled = !changed;
      discard.disabled = !changed;
    };

    select.addEventListener("change", render);
    form.addEventListener("reset", () => window.setTimeout(render, 0));
    render();
  }
})();
