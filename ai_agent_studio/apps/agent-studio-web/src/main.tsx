import React from 'react';
import { createRoot } from 'react-dom/client';
import App from './ui/App';
import './ui/brand-font.css';
import './ui/elenio-tokens.css';
import './ui/styles.css';

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
