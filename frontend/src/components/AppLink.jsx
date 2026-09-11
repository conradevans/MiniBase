export default function AppLink({
  href,
  navigate,
  children,
  onClick,
  ...props
}) {
  return (
    <a
      href={href}
      onClick={(event) => {
        onClick?.(event)
        if (
          event.defaultPrevented ||
          event.button !== 0 ||
          event.metaKey ||
          event.ctrlKey ||
          event.shiftKey ||
          event.altKey
        ) {
          return
        }
        event.preventDefault()
        navigate(href)
      }}
      {...props}
    >
      {children}
    </a>
  )
}
