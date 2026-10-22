let count = import.meta.webpackHot?.data?.count || 0;
const root = document.getElementById('app');
root.innerHTML = '<h1 data-testid="marker">compat-before</h1><button data-testid="counter"></button>';
const button = root.querySelector('button');
button.textContent = count;
button.onclick = () => { button.textContent = ++count; };
if (import.meta.webpackHot) {
  import.meta.webpackHot.accept();
  import.meta.webpackHot.dispose(data => { data.count = count; });
}
