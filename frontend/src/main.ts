import { mount } from "svelte";
import '../style.css';
import App from './App.svelte';
import appIcon from '@assets/PMT-SquareIcon-Mint.png?url';

const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
if (link) link.href = appIcon;

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
