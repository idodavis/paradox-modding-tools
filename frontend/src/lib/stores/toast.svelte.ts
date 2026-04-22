import { writable } from 'svelte/store';

export interface ToastOptions {
  message?: string;
  contentHtml?: string;
  type?: 'alert-info' | 'alert-success' | 'alert-warning' | 'alert-error';
  placement?: 'toast-start' | 'toast-end' | 'toast-center' | 'toast-top' | 'toast-middle' | 'toast-bottom';
  duration?: number;
}

export interface ToastItem extends ToastOptions {
  id: string;
}

const toasts = writable<ToastItem[]>([]);

export { toasts };

export function showToast(options: ToastOptions) {
  const id = crypto.randomUUID?.() ?? String(Date.now());
  const duration = options.duration ?? 6000;
  const placement = options.placement ?? 'toast-end';
  const type = options.type ?? 'alert-info';

  const item: ToastItem = {
    id,
    message: options.message,
    contentHtml: options.contentHtml,
    type,
    placement,
    duration,
  };

  toasts.update((list) => [...list, item]);

  if (duration > 0) {
    setTimeout(() => {
      toasts.update((list) => list.filter((t) => t.id !== id));
    }, duration);
  }
}