mergeInto(LibraryManager.library, {
  VapourQuit: function () {
    try {
      if (window.vapour && typeof window.vapour.quit === "function") {
        window.vapour.quit(0);
      }
    } catch (_) {}
  },
});
