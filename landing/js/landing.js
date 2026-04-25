// Landing page interactions: mobile nav toggle, footer year, demo form submit, currency detection.
(function () {
  'use strict';

  // --- mobile nav -------------------------------------------------------
  var toggle = document.querySelector('.nav-toggle');
  var mobileNav = document.getElementById('mobile-nav');
  if (toggle && mobileNav) {
    toggle.addEventListener('click', function () {
      var open = mobileNav.dataset.open === 'true';
      mobileNav.hidden = open;
      mobileNav.dataset.open = open ? 'false' : 'true';
      toggle.setAttribute('aria-expanded', open ? 'false' : 'true');
    });
    // Close after click on link.
    mobileNav.addEventListener('click', function (e) {
      if (e.target instanceof HTMLAnchorElement) {
        mobileNav.hidden = true;
        mobileNav.dataset.open = 'false';
        toggle.setAttribute('aria-expanded', 'false');
      }
    });
  }

  // --- footer year ------------------------------------------------------
  var yearEl = document.getElementById('year');
  if (yearEl) yearEl.textContent = String(new Date().getFullYear());

  // --- currency detection based on IP geolocation -----------------------
  function detectCurrency() {
    var priceElements = document.querySelectorAll('.price[data-currency]');
    if (priceElements.length === 0) return;

    // Currency rates relative to USD
    var rates = {
      USD: { symbol: '$', rate: 1 },
      GBP: { symbol: '£', rate: 0.79 },
      AED: { symbol: 'د.إ', rate: 3.67 }
    };

    // Country to currency mapping
    var countryToCurrency = {
      // USD - US, Canada
      'US': 'USD', 'CA': 'USD',
      // GBP - UK, Europe
      'GB': 'GBP', 'IE': 'GBP', 'FR': 'GBP', 'DE': 'GBP', 'IT': 'GBP', 'ES': 'GBP',
      'NL': 'GBP', 'BE': 'GBP', 'AT': 'GBP', 'CH': 'GBP', 'PT': 'GBP', 'LU': 'GBP',
      // AED - UAE, Middle East
      'AE': 'AED', 'SA': 'AED', 'QA': 'AED', 'KW': 'AED', 'BH': 'AED', 'OM': 'AED'
    };

    // Try to get country from geolocation API
    fetch('https://ipapi.co/json/')
      .then(function (resp) {
        if (!resp.ok) throw new Error('status ' + resp.status);
        return resp.json();
      })
      .then(function (data) {
        var countryCode = data.country_code;
        var currency = countryToCurrency[countryCode] || 'USD';
        var currencyData = rates[currency];

        priceElements.forEach(function (el) {
          var tier = el.getAttribute('data-currency');
          var basePriceUSD = 0;

          // Base prices in USD
          if (tier === 'enterprise') basePriceUSD = 250;
          else if (tier === 'business') basePriceUSD = 12;
          else if (tier === 'starter') basePriceUSD = 0;

          var localPrice = basePriceUSD * currencyData.rate;
          var priceText = basePriceUSD === 0
            ? currencyData.symbol + '0'
            : currencyData.symbol + localPrice.toFixed(0);

          el.innerHTML = '<strong>' + priceText + '</strong><span> / host / month</span>';
        });
      })
      .catch(function () {
        // Fallback to USD on error
        console.log('Currency detection failed, using USD');
      });
  }

  // Run currency detection on page load
  detectCurrency();

  // --- scroll animations (fade-in effects) -------------------------------
  function initScrollAnimations() {
    if (typeof IntersectionObserver === 'undefined') return;

    var observerOptions = {
      root: null,
      rootMargin: '0px',
      threshold: 0.1
    };

    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add('fade-in');
          observer.unobserve(entry.target);
        }
      });
    }, observerOptions);

    // Observe sections
    var sections = document.querySelectorAll('section');
    sections.forEach(function (section) {
      section.style.opacity = '0';
      section.style.transform = 'translateY(20px)';
      section.style.transition = 'opacity 0.6s ease-out, transform 0.6s ease-out';
      observer.observe(section);
    });
  }

  // Initialize scroll animations
  initScrollAnimations();

  // --- demo form: graceful submit + status ------------------------------
  var form = document.getElementById('demo-form');
  var statusEl = document.getElementById('form-status');
  if (form && statusEl) {
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var data = new FormData(form);
      var endpoint = form.getAttribute('action') || '';
      // Default endpoint is a placeholder; production wires CONTACT_ENDPOINT.
      if (!endpoint || endpoint.indexOf('placeholder') !== -1) {
        statusEl.textContent = 'Email sales@cloudspacetechs.com — we reply within one business day.';
        statusEl.style.color = '#9aa0ad';
        form.reset();
        return;
      }
      statusEl.textContent = 'Sending…';
      statusEl.style.color = '#9aa0ad';
      fetch(endpoint, {
        method: 'POST',
        body: data,
        headers: { Accept: 'application/json' },
      })
        .then(function (resp) {
          if (!resp.ok) throw new Error('status ' + resp.status);
          statusEl.textContent = 'Thanks — we\'ll be in touch within one business day.';
          statusEl.style.color = '#58cf85';
          form.reset();
        })
        .catch(function () {
          statusEl.textContent = 'Could not send. Email sales@cloudspacetechs.com instead.';
          statusEl.style.color = '#f57878';
        });
    });
  }
})();
