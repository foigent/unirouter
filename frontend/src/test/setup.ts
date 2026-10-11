import '@testing-library/jest-dom'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Existing page fixtures choose Chinese explicitly; bootstrap locale tests
// clear this preference to exercise the product's English default.
localStorage.setItem('unirouter.ui.locale', 'zh-CN')

afterEach(() => {
  cleanup()
})

// Polyfill ResizeObserver for Semi Design components in jsdom
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class ResizeObserver {
    callback: ResizeObserverCallback;
    constructor(callback: ResizeObserverCallback) {
      this.callback = callback;
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

// Polyfill matchMedia for Semi Design grid/layout components
if (typeof window !== 'undefined' && !window.matchMedia) {
  (window as any).matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}
