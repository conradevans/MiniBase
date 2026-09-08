const destinations = {
  root: [
    ["ReactorLab", "https://reactorlab.dev/"],
    ["MiniDeploy", "https://minideploy.reactorlab.dev/"],
    ["MiniBase", "https://minibase.reactorlab.dev/"],
  ],
  admin: [
    ["ReactorLab", "https://reactorlab.dev/admin"],
    ["MiniDeploy", "https://minideploy.reactorlab.dev/admin/"],
    ["MiniBase", "https://minibase.reactorlab.dev/admin"],
    ["MiniAI", "https://miniai.reactorlab.dev/admin"],
  ],
  guest: [
    ["ReactorLab", "https://reactorlab.dev/guest"],
    ["MiniDeploy", "https://minideploy.reactorlab.dev/guest/"],
    ["MiniBase", "https://minibase.reactorlab.dev/guest"],
  ],
}

export default function ProductNav({ mode = "root" }) {
  return (
    <nav className="product-nav" aria-label="ReactorLab products">
      {destinations[mode].map(([name, href]) => (
        <a
          key={name}
          className={name === "MiniBase" ? "product-link active" : "product-link"}
          href={href}
          aria-current={name === "MiniBase" ? "page" : undefined}
        >
          {name}
        </a>
      ))}
    </nav>
  )
}
