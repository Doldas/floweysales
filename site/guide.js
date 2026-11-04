'use strict';
// Theme toggle (same storage key as the app) and a title/heading filter for the guide navigation.
(function () {
  var root = document.documentElement;
  var button = document.getElementById('theme');
  if (button) {
    button.addEventListener('click', function () {
      var dark = root.getAttribute('data-theme') === 'dark' ||
        (!root.getAttribute('data-theme') && window.matchMedia('(prefers-color-scheme: dark)').matches);
      var next = dark ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('fs.theme', next); } catch (e) { /* storage may be blocked */ }
    });
  }
  var input = document.getElementById('guide-search');
  var list = document.getElementById('guide-list');
  var empty = document.getElementById('guide-empty');
  if (!input || !list) return;
  var index = [];
  fetch('search.json').then(function (r) { return r.json(); }).then(function (data) { index = data; }).catch(function () { index = []; });
  input.addEventListener('input', function () {
    var words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    var shown = 0;
    Array.prototype.forEach.call(list.children, function (li) {
      var slug = li.querySelector('a').getAttribute('href').replace('.html', '');
      var entry = index.filter(function (e) { return e.slug === slug; })[0];
      var text = (li.textContent + ' ' + (entry ? entry.summary + ' ' + entry.headings.join(' ') : '')).toLowerCase();
      var match = words.every(function (w) { return text.indexOf(w) !== -1; });
      li.hidden = !match;
      if (match) shown++;
    });
    if (empty) empty.hidden = shown > 0;
  });
})();
