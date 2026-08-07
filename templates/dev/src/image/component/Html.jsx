import React from 'react';
import css_output from '../../assets/main.css';
import { LinkStyleSheet } from 'jsxp';
export default function Html({ children }) {
  return (
    <html>
      <head>
        <LinkStyleSheet src={css_output} />
      </head>
      <body>{children}</body>
    </html>
  );
}
