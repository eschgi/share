// Runs in <head>, before the page is drawn: shows the theme chosen in this browser, so the page
// never flashes in another one. It's a file of its own because the Content-Security-Policy
// allows no inline scripts, and plain JavaScript, served as it is, because it runs before the
// modules. It repeats src/theme.ts's rules; test/theme.test.ts checks that both say the same.
(function () {
  var colors = {
    ember: '#16120f',
    midnight: '#0f141b',
    moss: '#111512',
    plum: '#151118',
    black: '#000000',
    linen: '#f6f0e9',
    frost: '#f3f6fa',
  };
  var choice = null;
  try {
    choice = localStorage.getItem('share.theme');
  } catch (e) {
    // no storage: the default
  }
  if (choice !== 'auto' && !Object.prototype.hasOwnProperty.call(colors, choice)) choice = 'ember';
  var theme = choice;
  if (choice === 'auto') theme = matchMedia('(prefers-color-scheme: light)').matches ? 'linen' : 'ember';
  document.documentElement.setAttribute('data-theme', choice);
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', colors[theme]);
  var scheme = document.querySelector('meta[name="color-scheme"]');
  if (scheme) scheme.setAttribute('content', theme === 'linen' || theme === 'frost' ? 'light' : 'dark');
})();
