/* ============================================
   KubeCopilot Website — JS
   ============================================ */

document.addEventListener("DOMContentLoaded", function () {
  // --- Mobile nav toggle ---
  var toggle = document.getElementById("nav-toggle");
  var links = document.getElementById("nav-links");

  if (toggle && links) {
    toggle.addEventListener("click", function () {
      links.classList.toggle("open");
      var expanded = links.classList.contains("open");
      toggle.setAttribute("aria-expanded", String(expanded));
    });

    // Close menu on link click
    links.querySelectorAll("a").forEach(function (link) {
      link.addEventListener("click", function () {
        links.classList.remove("open");
        toggle.setAttribute("aria-expanded", "false");
      });
    });
  }

  // --- Navbar background on scroll ---
  var navbar = document.getElementById("navbar");
  if (navbar) {
    window.addEventListener("scroll", function () {
      if (window.scrollY > 60) {
        navbar.style.background = "rgba(10, 14, 26, 0.95)";
      } else {
        navbar.style.background = "rgba(10, 14, 26, 0.85)";
      }
    });
  }

  // --- Screenshot tabs ---
  var tabButtons = document.querySelectorAll(".tab-btn");
  var screenshotImg = document.getElementById("screenshot-img");

  tabButtons.forEach(function (btn) {
    btn.addEventListener("click", function () {
      tabButtons.forEach(function (b) { b.classList.remove("active"); });
      btn.classList.add("active");

      var src = btn.getAttribute("data-src");
      var alt = btn.getAttribute("data-alt");
      if (src && screenshotImg) {
        screenshotImg.src = src;
        screenshotImg.alt = alt || "";
      }
    });
  });

  // --- Smooth scroll for anchor links ---
  var prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  document.querySelectorAll('a[href^="#"]').forEach(function (anchor) {
    anchor.addEventListener("click", function (e) {
      var href = this.getAttribute("href");
      if (!href || href === "#") return;
      try {
        var target = document.querySelector(href);
      } catch (_) {
        return;
      }
      if (target) {
        e.preventDefault();
        target.scrollIntoView({ behavior: prefersReducedMotion ? "auto" : "smooth", block: "start" });
      }
    });
  });
});
