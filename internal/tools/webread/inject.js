(async () => {
    loadExternalScript=(src)=>  {
        return new Promise((resolve, reject) => {
            const script = document.createElement('script');
            script.src = src;
            script.async = true; // Prevents parser-blocking

            // Fire resolve once the script completely executes
            script.onload = () => resolve(script);
            script.onerror = () => reject(new Error(`Script load error for ${src}`));

            document.head.appendChild(script);
        });
    }

    await loadExternalScript('https://cdnjs.cloudflare.com/ajax/libs/readability/0.6.0/Readability.js')
    console.log("loaded")

    const documentCloned = document.cloneNode(true)
    const reader = new Readability(documentCloned)

    return reader.parse()
})