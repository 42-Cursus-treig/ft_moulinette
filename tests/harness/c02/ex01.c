#include <stdlib.h>
#include <unistd.h>

char	*ft_strncpy(char *dest, char *src, unsigned int n);

int	main(int argc, char **argv)
{
	char			dest[256];
	unsigned int	n;
	unsigned int	i;

	if (argc < 3)
		return (1);
	n = (unsigned int)atoi(argv[2]);
	i = 0;
	while (i < sizeof(dest))
	{
		dest[i] = 'Z';
		i++;
	}
	ft_strncpy(dest, argv[1], n);
	write(1, dest, n);
	return (0);
}
