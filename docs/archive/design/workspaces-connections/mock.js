// Mockup chrome only: light/dark toggle. Not product code.
(function () {
  var root = document.documentElement
  var theme = null
  try { theme = new URLSearchParams(location.search).get('theme') } catch (e) {}
  try { if (!theme) theme = localStorage.getItem('vibe-mock-theme') } catch (e) {}
  if (!theme) theme = (window.matchMedia && matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light'
  root.setAttribute('data-theme', theme)
  function set(t) {
    root.setAttribute('data-theme', t)
    try { localStorage.setItem('vibe-mock-theme', t) } catch (e) {}
    var lab = document.getElementById('theme-label')
    if (lab) lab.textContent = t === 'dark' ? 'Dark' : 'Light'
    var f = document.querySelectorAll('iframe[data-src]')
    for (var i = 0; i < f.length; i++) f[i].src = f[i].getAttribute('data-src') + '?theme=' + t
  }
  document.addEventListener('DOMContentLoaded', function () {
    var b = document.getElementById('theme-toggle')
    set(root.getAttribute('data-theme'))
    if (b) b.addEventListener('click', function () { set(root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark') })
    var links = document.querySelectorAll('a[data-keep-theme]')
    for (var j = 0; j < links.length; j++) links[j].addEventListener('click', function (e) {
      e.currentTarget.search = '?theme=' + root.getAttribute('data-theme')
    })
  })
})()
