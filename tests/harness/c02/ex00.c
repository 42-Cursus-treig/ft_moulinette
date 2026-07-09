#include <unistd.h>

char	*ft_strcpy(char *dest, char *src);

static int	my_strlen(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	return (i);
}

int	main(int argc, char **argv)
{
	char	dest[256];

	if (argc < 2)
		return (1);
	dest[0] = '\0';
	ft_strcpy(dest, argv[1]);
	write(1, dest, my_strlen(dest));
	return (0);
}
