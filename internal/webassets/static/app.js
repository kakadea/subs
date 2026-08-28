(() => {
  const copyButtons = document.querySelectorAll("[data-copy-target]");
  for (const button of copyButtons) {
    button.addEventListener("click", async () => {
      const target = document.getElementById(button.dataset.copyTarget || "");
      if (!target) return;
      try {
        await navigator.clipboard.writeText(target.value);
        const original = button.textContent;
        button.textContent = "Copiado";
        window.setTimeout(() => { button.textContent = original; }, 1800);
      } catch {
        target.focus();
        target.select();
      }
    });
  }
})();
